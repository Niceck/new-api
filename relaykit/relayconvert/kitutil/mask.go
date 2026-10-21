package kitutil

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	maskURLPattern    = regexp.MustCompile(`(http|https)://[^\s/$.?#].[^\s]*`)
	maskDomainPattern = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)
	maskIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// maskApiKeyPattern matches patterns like 'api_key:xxx' or "api_key:xxx" to mask the API key value
	maskApiKeyPattern = regexp.MustCompile(`(['"]?)api_key:([^\s'"]+)(['"]?)`)
)

// Upstream errors quote request-parameter paths (thinking.type=adaptive,
// messages.3.content.0) that look like domains; masking them hides which parameter
// the customer has to fix. Only these exact shapes are kept, so no real host passes:
//
//	thinking.type[.adaptive|.disabled|.enabled]
//	messages|tools|system.<index>[.<known field>|.<index>]...
var (
	requestFieldRoots  = map[string]bool{"messages": true, "tools": true, "system": true}
	thinkingTypeValues = map[string]bool{"adaptive": true, "disabled": true, "enabled": true}
	requestFieldNames  = map[string]bool{
		"content": true, "text": true, "image": true, "source": true, "data": true,
		"base64": true, "input": true, "custom": true, "type": true, "document": true,
		"signature": true, "thinking": true, "citations": true, "title": true, "context": true,
	}
)

// isFieldPathBoundary reports whether c may sit right before or after a request
// parameter path. Anything else (dots, hyphens, letters, non-ASCII bytes) could be
// part of a longer host name that the ASCII-only pattern cut short.
func isFieldPathBoundary(c byte) bool {
	return strings.IndexByte(" \t\r\n\"'`=:,;()[]{}<>", c) >= 0
}

// fieldPathTailEnd skips the continuations the pattern stops at (".0" in
// messages.3.content.0, ".between_tools" in thinking.type.between_tools) and returns
// where the path ends, or -1 when a following segment could be a host label: only
// all-digit or snake_case ASCII segments are accepted.
func fieldPathTailEnd(str string, end int) int {
	for end < len(str) && str[end] == '.' {
		j := end + 1
		digits, underscore := true, false
		for j < len(str) && (isASCIIAlnum(str[j]) || str[j] == '_') {
			if str[j] < '0' || str[j] > '9' {
				digits = false
			}
			if str[j] == '_' {
				underscore = true
			}
			j++
		}
		if j == end+1 || !(digits || underscore) {
			return -1
		}
		end = j
	}
	return end
}

func isASCIIAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isRequestFieldPath reports whether the domain-shaped match str[start:end] is one of
// the request-parameter path shapes above and is delimited on both sides, so it cannot
// be a fragment of a host name.
func isRequestFieldPath(str string, start, end int) bool {
	if start > 0 && !isFieldPathBoundary(str[start-1]) {
		return false
	}
	tail := fieldPathTailEnd(str, end)
	if tail < 0 || (tail < len(str) && !isFieldPathBoundary(str[tail])) {
		return false
	}
	labels := strings.Split(str[start:end], ".")
	if len(labels) < 2 {
		return false
	}
	if labels[0] == "thinking" {
		return labels[1] == "type" && (len(labels) == 2 || (len(labels) == 3 && thinkingTypeValues[labels[2]]))
	}
	if !requestFieldRoots[labels[0]] || !isDigits(labels[1]) {
		return false
	}
	for _, label := range labels[2:] {
		if !isDigits(label) && !requestFieldNames[label] {
			return false
		}
	}
	return true
}

// maskPlainDomains masks domain names without protocol, keeping request-parameter paths.
func maskPlainDomains(str string) string {
	matches := maskDomainPattern.FindAllStringIndex(str, -1)
	if len(matches) == 0 {
		return str
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(str[last:m[0]])
		if isRequestFieldPath(str, m[0], m[1]) {
			b.WriteString(str[m[0]:m[1]])
		} else {
			b.WriteString(maskHostForPlainDomain(str[m[0]:m[1]]))
		}
		last = m[1]
	}
	b.WriteString(str[last:])
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maskHostTail returns the tail parts of a domain/host that should be preserved.
// It keeps 2 parts for likely country-code TLDs (e.g., co.uk, com.cn), otherwise keeps only the TLD.
func maskHostTail(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]
	if len(lastPart) == 2 && len(secondLastPart) <= 3 {
		// Likely country code TLD like co.uk, com.cn
		return []string{secondLastPart, lastPart}
	}
	return []string{lastPart}
}

// maskHostForURL collapses subdomains and keeps only masked prefix + preserved tail.
// Example: api.openai.com -> ***.com, sub.domain.co.uk -> ***.co.uk
func maskHostForURL(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "***"
	}
	tail := maskHostTail(parts)
	return "***." + strings.Join(tail, ".")
}

// maskHostForPlainDomain masks a plain domain and reflects subdomain depth with multiple ***.
// Example: openai.com -> ***.com, api.openai.com -> ***.***.com, sub.domain.co.uk -> ***.***.co.uk
func maskHostForPlainDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	tail := maskHostTail(parts)
	numStars := len(parts) - len(tail)
	if numStars < 1 {
		numStars = 1
	}
	stars := strings.TrimSuffix(strings.Repeat("***.", numStars), ".")
	return stars + "." + strings.Join(tail, ".")
}

// MaskSensitiveInfo masks sensitive information like URLs, IPs, and domain names in a string
// Example:
// http://example.com -> http://***.com
// https://api.test.org/v1/users/123?key=secret -> https://***.org/***/***/?key=***
// https://sub.domain.co.uk/path/to/resource -> https://***.co.uk/***/***
// 192.168.1.1 -> ***.***.***.***
// openai.com -> ***.com
// www.openai.com -> ***.***.com
// api.openai.com -> ***.***.com
func MaskSensitiveInfo(str string) string {
	// Mask URLs
	str = maskURLPattern.ReplaceAllStringFunc(str, func(urlStr string) string {
		u, err := url.Parse(urlStr)
		if err != nil {
			return urlStr
		}

		host := u.Host
		if host == "" {
			return urlStr
		}

		// Mask host with unified logic
		maskedHost := maskHostForURL(host)

		result := u.Scheme + "://" + maskedHost

		// Mask path
		if u.Path != "" && u.Path != "/" {
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			maskedPathParts := make([]string, len(pathParts))
			for i := range pathParts {
				if pathParts[i] != "" {
					maskedPathParts[i] = "***"
				}
			}
			if len(maskedPathParts) > 0 {
				result += "/" + strings.Join(maskedPathParts, "/")
			}
		} else if u.Path == "/" {
			result += "/"
		}

		// Mask query parameters
		if u.RawQuery != "" {
			values, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				// If can't parse query, just mask the whole query string
				result += "?***"
			} else {
				maskedParams := make([]string, 0, len(values))
				for key := range values {
					maskedParams = append(maskedParams, key+"=***")
				}
				if len(maskedParams) > 0 {
					result += "?" + strings.Join(maskedParams, "&")
				}
			}
		}

		return result
	})

	// Mask domain names without protocol (like openai.com, www.openai.com)
	str = maskPlainDomains(str)

	// Mask IP addresses
	str = maskIPPattern.ReplaceAllString(str, "***.***.***.***")

	// Mask API keys (e.g., "api_key:AIzaSyAAAaUooTUni8AdaOkSRMda30n_Q4vrV70" -> "api_key:***")
	str = maskApiKeyPattern.ReplaceAllString(str, "${1}api_key:***${3}")

	return str
}
