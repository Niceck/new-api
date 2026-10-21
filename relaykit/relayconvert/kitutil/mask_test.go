package kitutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 上游报错里的请求参数路径（thinking.type、messages.3.content）长得像域名，
// 以前被当成域名遮掉，客户看不到是哪个参数出了问题。
func TestMaskSensitiveInfoKeepsRequestFieldPaths(t *testing.T) {
	cases := []string{
		"claude-opus-5-5 requires adaptive thinking; omit thinking or use thinking.type=adaptive and output_config.effort",
		`"thinking.type.disabled" is not supported for this model. Use "thinking.type.adaptive" and "output_config.effort" to control thinking behavior.`,
		"claude-sonnet-5-5 requires adaptive thinking or thinking.type=between_tools; omit thinking or use one of those modes",
		"messages.3.content.0: Invalid `signature` in `thinking` block",
		"messages.12.content.1.image.source.base64.data: The image was specified using the image/jpeg media type",
		"tools.0.custom.input_schema: JSON schema is invalid",
		"system.1.text: cache_control cannot be set for empty text blocks",
	}
	for _, input := range cases {
		assert.Equal(t, input, MaskSensitiveInfo(input))
	}
}

// 放行参数路径不能顺带放出上游的域名、地址与 IP。
func TestMaskSensitiveInfoStillMasksHosts(t *testing.T) {
	cases := map[string]string{
		"dial tcp: lookup cc-hq.freemodel.dev: no such host":     "dial tcp: lookup ***.***.dev: no such host",
		"upstream stable.monkeyapi.net refused":                  "upstream ***.***.net refused",
		"request https://cc-hq.freemodel.dev/v1/messages failed": "request https://***.dev/***/*** failed",
		"call api.openai.com failed":                             "call ***.***.com failed",
		"connect 198.13.49.40:443 timeout":                       "connect ***.***.***.***:443 timeout",
		"messages.example.com is down":                           "***.***.com is down",
		"thinking.freemodel.dev is down":                         "***.***.dev is down",
		"thinking.type.com is down":                              "***.***.com is down",
		"messages.1.example.com is down":                         "***.***.***.com is down",
		"system.0.relay.ai is down":                              "***.***.***.ai is down",
		"tools.example.io is down":                               "***.***.io is down",
		"see openai.com":                                         "see ***.com",
		"thinking.example.co.uk is down":                         "***.***.co.uk is down",
		// Codex 审出的放行绕过：TLD 不在清单里、Punycode 截断、大写协议、非法端口。
		"dial tcp: lookup messages.1.vendor.network:443 failed": "dial tcp: lookup ***.***.***.network:443 failed",
		"lookup thinking.type.xn--p1ai failed":                  "lookup ***.***.xn--p1ai failed",
		"HTTPS://thinking.type.network/v1":                      "HTTPS://***.***.network/v1",
		"https://thinking.type.network:bad/v1":                  "https://***.***.network:bad/v1",
		"thinking.type.adaptive.vendor.com is down":             "***.***.***.***.com is down",
		"messages.0.content.vendor.network is down":             "***.***.***.***.network is down",
		"thinking.type.enabled-vendor.network is down":          "***.***.***.network is down",
		"tools.0.custom.xn--p1ai is down":                       "***.***.***.xn--p1ai is down",
	}
	for input, want := range cases {
		assert.Equal(t, want, MaskSensitiveInfo(input), input)
	}
}
