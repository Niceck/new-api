package operation_setting

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	// MaxGroupEntryGroups 是 group_entry_hosts 里最多能登记的分组数。
	MaxGroupEntryGroups = 64
	// MaxGroupEntryHostsPerGroup 是一个分组最多能登记的入口主机名数。
	MaxGroupEntryHostsPerGroup = 16
	// MaxGroupEntryRejectMessageRunes 是自定义拒绝提示的长度上限（字）。
	MaxGroupEntryRejectMessageRunes = 300

	groupEntryPlaceholderGroup = "{group}"
	groupEntryPlaceholderEntry = "{entry}"

	defaultGroupEntryRejectMessage        = "当前接口地址不支持 {group} 分组，请把接口地址（Base URL）改为 https://{entry} 后重试"
	defaultGroupEntryRejectMessageNoEntry = "当前接口地址不支持 {group} 分组"
)

// 规范写法的主机名：小写，按点分段，每段首尾是字母或数字。协议头、端口、路径、
// 结尾的点、大写都不算规范写法——它们和规范写法指向同一个入口，放进同一张表只会让人
// 以为登记上了而实际对不上。
var groupEntryHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

var groupEntryPlaceholderRe = regexp.MustCompile(`\{[^{}]*\}`)

// groupEntryPolicy 是请求路径读取的只读快照。
//
// 和 streamHeaderPolicySnapshot 同理：配置框架用反射直接改 generalSetting 的字段，
// 请求路径读快照而不读字段，快照发布后不再修改。
type groupEntryPolicy struct {
	groups  map[string]groupEntry
	message string
}

// groupEntry 是一个受限分组的入口：允许的主机名集合，以及登记顺序里的第一个（进报错提示）。
type groupEntry struct {
	allowed map[string]struct{}
	first   string
}

var groupEntryPolicySnapshot atomic.Pointer[groupEntryPolicy]

func usableGroupEntryGroup(group string) bool {
	return group != "" && group == strings.TrimSpace(group) && group != "auto"
}

func usableGroupEntryHost(host string) bool {
	return len(host) <= 253 && groupEntryHostRe.MatchString(host)
}

// normalizeEntryHost 把请求的 Host 整理成可比较的主机名：去掉端口、结尾的点，转小写。
// 带端口的地址和不带端口的地址是同一个入口。整理不出来的原样返回，自然对不上任何登记值。
func normalizeEntryHost(requestHost string) string {
	host := strings.TrimSpace(requestHost)
	if h, port, err := net.SplitHostPort(host); err == nil && isDigits(port) {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	lowered := make([]byte, len(host))
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c >= 0x80 {
			// 非 ASCII 的 Host 不整理：strings.ToLower 会把个别 Unicode 字符折成 ASCII 字母
			// （如开尔文符号 U+212A 折成 k），那样就能拼出登记值
			return host
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lowered[i] = c
	}
	return string(lowered)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// RefreshGroupEntryHosts 把 general_setting 里的入口限制发布成快照。
// 调用方须是唯一的写入者（配置更新持有 OptionMap 写锁时调用）。
//
// 库里能解析、但不合规的条目（直接改库不经过校验）只会让限制更严，不会更松：
// 不合规的主机名不发布；一个分组登记的主机名全都不合规时，它在哪个入口都用不了，
// 这样配错了会立刻被发现，而不是悄悄变成「没限制」。
// 整个值解析不了时配置框架会跳过它、字段保持原值——那种情况由 model 包在加载时记错误日志。
func RefreshGroupEntryHosts() {
	policy := &groupEntryPolicy{
		groups:  make(map[string]groupEntry, len(generalSetting.GroupEntryHosts)),
		message: generalSetting.GroupEntryRejectMessage,
	}
	for group, hosts := range generalSetting.GroupEntryHosts {
		if !usableGroupEntryGroup(group) {
			continue
		}
		entry := groupEntry{allowed: make(map[string]struct{}, len(hosts))}
		for _, host := range hosts {
			if !usableGroupEntryHost(host) {
				continue
			}
			if len(entry.allowed) == 0 {
				entry.first = host
			}
			entry.allowed[host] = struct{}{}
		}
		policy.groups[group] = entry
	}
	groupEntryPolicySnapshot.Store(policy)
}

// GroupAllowedOnHost 返回这个分组能不能经由 requestHost（请求的 Host，可带端口）使用。
// 没登记的分组不受限；登记了的分组只能走登记的入口。
func GroupAllowedOnHost(group, requestHost string) bool {
	policy := groupEntryPolicySnapshot.Load()
	if policy == nil {
		return true
	}
	entry, restricted := policy.groups[group]
	if !restricted {
		return true
	}
	_, ok := entry.allowed[normalizeEntryHost(requestHost)]
	return ok
}

// GroupEntryRejectMessage 返回分组走错入口时回给客户端的提示；分组不受限时返回空串。
// 提示里的入口取登记顺序里的第一个，所以对客地址要排在最前。
func GroupEntryRejectMessage(group string) string {
	policy := groupEntryPolicySnapshot.Load()
	if policy == nil {
		return ""
	}
	groupEntry, restricted := policy.groups[group]
	if !restricted {
		return ""
	}
	entry := groupEntry.first
	template := policy.message
	if strings.TrimSpace(template) == "" {
		template = defaultGroupEntryRejectMessage
		if entry == "" {
			template = defaultGroupEntryRejectMessageNoEntry
		}
	}
	return strings.NewReplacer(groupEntryPlaceholderGroup, group, groupEntryPlaceholderEntry, entry).Replace(template)
}

// ParseGroupEntryHosts 校验 group_entry_hosts 的待写入值并返回解析结果。
// 配置框架对解析失败是静默跳过的，不在写入前拦住，接口会报成功而实际没生效。
// 分组是否存在由调用方核对（这个包不依赖分组倍率表）。
func ParseGroupEntryHosts(value string) (map[string][]string, error) {
	var parsed map[string]*[]*string // 用指针区分「没写值（null）」和空
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
		return nil, fmt.Errorf("group_entry_hosts 必须是 {\"分组\": [\"主机名\", ...]} 形式的 JSON 对象: %w", err)
	}
	if parsed == nil {
		return nil, fmt.Errorf("group_entry_hosts 必须是 JSON 对象，清空请写 {}")
	}
	if len(parsed) > MaxGroupEntryGroups {
		return nil, fmt.Errorf("最多登记 %d 个分组", MaxGroupEntryGroups)
	}
	groups := make([]string, 0, len(parsed))
	for group := range parsed {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	entries := make(map[string][]string, len(parsed))
	for _, group := range groups {
		if group == "auto" {
			return nil, fmt.Errorf("auto 不是具体分组，不能登记入口")
		}
		if !usableGroupEntryGroup(group) {
			return nil, fmt.Errorf("分组名 %q 不能为空，首尾也不能有空白", group)
		}
		hosts := parsed[group]
		if hosts == nil {
			return nil, fmt.Errorf("分组 %s 的主机名列表不能为 null，取消限制请删掉这个分组", group)
		}
		if len(*hosts) == 0 {
			return nil, fmt.Errorf("分组 %s 至少要有一个主机名，取消限制请删掉这个分组", group)
		}
		if len(*hosts) > MaxGroupEntryHostsPerGroup {
			return nil, fmt.Errorf("分组 %s 最多登记 %d 个主机名", group, MaxGroupEntryHostsPerGroup)
		}
		seen := make(map[string]struct{}, len(*hosts))
		cleaned := make([]string, 0, len(*hosts))
		for _, host := range *hosts {
			if host == nil {
				return nil, fmt.Errorf("分组 %s 的主机名不能为 null", group)
			}
			if !usableGroupEntryHost(*host) {
				return nil, fmt.Errorf("分组 %s 的主机名 %q 不是规范写法（只写小写域名，不带协议、端口、路径）", group, *host)
			}
			if _, dup := seen[*host]; dup {
				return nil, fmt.Errorf("分组 %s 的主机名 %s 重复", group, *host)
			}
			seen[*host] = struct{}{}
			cleaned = append(cleaned, *host)
		}
		entries[group] = cleaned
	}
	return entries, nil
}

// ValidateGroupEntryRejectMessage 校验 group_entry_reject_message 的待写入值。
// 空串表示用内置提示；占位符只认 {group} 与 {entry}。
func ValidateGroupEntryRejectMessage(value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("group_entry_reject_message 不能只有空白，用内置提示请写空串")
	}
	if utf8.RuneCountInString(value) > MaxGroupEntryRejectMessageRunes {
		return fmt.Errorf("group_entry_reject_message 须在 %d 字以内", MaxGroupEntryRejectMessageRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("group_entry_reject_message 不能含换行等控制字符")
		}
	}
	for _, placeholder := range groupEntryPlaceholderRe.FindAllString(value, -1) {
		if placeholder != groupEntryPlaceholderGroup && placeholder != groupEntryPlaceholderEntry {
			return fmt.Errorf("不认识的占位符 %s，只支持 {group} 与 {entry}", placeholder)
		}
	}
	return nil
}
