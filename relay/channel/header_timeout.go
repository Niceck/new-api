package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	common2 "github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// errResponseHeaderTimeout 表示上游在期限内没有回响应头。
//
// 背景：上游偶发把请求挂住几分钟不回响应头。中转的 HTTP 客户端没有这一段的上限
// （RELAY_TIMEOUT 管的是整次请求，连流式正文一起限时；STREAMING_TIMEOUT 只管响应头
// 到达之后），于是既有的重试要等上游自己断开才触发，客户先拿到网关超时。
var errResponseHeaderTimeout = errors.New("upstream response header timeout")

// minStreamHeaderTimeout：剩余预算不足这么多时，视为预算已用尽。
const minStreamHeaderTimeout = time.Second

// streamHeaderWait 是一次尝试「等响应头」的安排。
type streamHeaderWait struct {
	timeout   time.Duration // 0 = 不限
	exhausted bool          // 总预算已用尽：这次尝试不该再发
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// streamHeaderWaitFor 给出这次尝试等上游响应头的安排。
//
// 只对被点名渠道的流式请求生效。设了总预算时，后面的尝试只拿得到剩余的预算，
// 这样「单次上限 × 尝试次数」不会越过前置网关的超时；预算用尽就不再发请求。
func streamHeaderWaitFor(c *gin.Context, info *common.RelayInfo) streamHeaderWait {
	if info == nil || !info.IsStream || info.ChannelMeta == nil {
		return streamHeaderWait{}
	}
	timeout := operation_setting.StreamHeaderTimeout(info.ChannelId)
	budget := operation_setting.StreamHeaderTotalBudget()
	start := requestEntryTime(c, info)
	if timeout <= 0 || budget <= 0 || start.IsZero() {
		return streamHeaderWait{timeout: timeout}
	}
	remaining := budget - time.Since(start)
	if remaining < minStreamHeaderTimeout {
		return streamHeaderWait{exhausted: true}
	}
	return streamHeaderWait{timeout: min(timeout, remaining)}
}

// requestEntryTime 返回总预算的起点：优先用入口中间件记下的进站时间；
// 不经过入口中间件的调用路径退回 RelayInfo.StartTime（记得偏晚，但好过没有）。
func requestEntryTime(c *gin.Context, info *common.RelayInfo) time.Time {
	if c != nil {
		if entry := common2.GetContextKeyTime(c, rootconstant.ContextKeyRequestEntryTime); !entry.IsZero() {
			return entry
		}
	}
	return info.StartTime
}

// doWithHeaderTimeout 发请求，并给「等响应头」这一段加上限。
// 响应头一到上限即作废，正文想读多久读多久；timeout <= 0 时等同 client.Do。
func doWithHeaderTimeout(client httpDoer, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		return client.Do(req)
	}
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(timeout, cancel)
	resp, err := client.Do(req.WithContext(ctx))
	// Stop 返回 false = 计时器已经触发：ctx 已取消（或马上取消），即使响应头恰好
	// 同时到达，正文也读不了了，一律按超时处理。
	if !timer.Stop() {
		if resp != nil {
			_ = resp.Body.Close()
		}
		cancel()
		return nil, fmt.Errorf("%w after %s", errResponseHeaderTimeout, timeout)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnCloseBody 在正文关闭时释放请求的 context。
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
