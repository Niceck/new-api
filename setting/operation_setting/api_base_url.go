package operation_setting

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MaxApiBaseURLLength 是 api_base_url 的长度上限。
const MaxApiBaseURLLength = 255

// 主机名：按点分段，每段首尾是字母或数字（大小写都收，浏览器会自己转小写）。
// 非 ASCII 域名要先转成 punycode 再填。
var apiBaseHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// ValidateApiBaseURL 校验 general_setting.api_base_url 的待写入值。
// 空串表示不设。其余只认 http(s)://主机[:端口]：前端会在后面拼 /v1，结尾带斜杠会拼出
// 「//v1」；这个值还会原样进 CC Switch 等客户端的一键导入链接，路径、查询串、账号密码
// 都不该出现。配置框架对解析失败是静默跳过的，所以要在写入前拦住。
func ValidateApiBaseURL(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > MaxApiBaseURLLength {
		return fmt.Errorf("api_base_url 不能超过 %d 个字符", MaxApiBaseURLLength)
	}
	if strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r >= 0x7f }) {
		return fmt.Errorf("api_base_url 只能含 ASCII 可见字符（不能有空白；中文域名请先转成 punycode）")
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("api_base_url 不是合法的地址: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("api_base_url 必须以 http:// 或 https:// 开头")
	}
	if u.Opaque != "" || u.Hostname() == "" {
		return fmt.Errorf("api_base_url 缺少主机名")
	}
	if u.User != nil {
		return fmt.Errorf("api_base_url 不能带账号密码")
	}
	if u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
		return fmt.Errorf("api_base_url 只写协议和主机（可带端口），不要带路径、结尾斜杠或查询串，例如 https://example.com")
	}
	if host := u.Hostname(); net.ParseIP(host) == nil && !apiBaseHostRe.MatchString(host) {
		return fmt.Errorf("api_base_url 的主机名 %q 不合法", host)
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("api_base_url 的端口为空：不带端口就去掉冒号")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("api_base_url 的端口 %q 不合法", port)
		}
	}
	return nil
}
