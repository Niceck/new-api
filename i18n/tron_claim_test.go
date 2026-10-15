package i18n

import (
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/require"
)

func TestTronClaimMessagesAreTranslatedInEveryLocale(t *testing.T) {
	require.NoError(t, Init())
	keys := []string{MsgTopupTronClaimTxNotFound, MsgTopupTronClaimWindowClosed, MsgTopupTronClaimLimitReached, MsgTopupTronClaimNotAllowed, MsgTopupTronClaimTooFrequent}
	for _, lang := range []string{LangZhCN, LangZhTW, LangEn} {
		for _, key := range keys {
			message, err := GetLocalizer(lang).Localize(&goi18n.LocalizeConfig{MessageID: key})
			require.NoError(t, err, "%s %s", lang, key)
			require.NotEmpty(t, message, "%s %s", lang, key)
			require.NotEqual(t, key, message, "%s %s", lang, key)
		}
	}
}
