package plugin

import (
	"net/http"
	"strings"
)

const (
	languageChinese = "zh"
	languageEnglish = "en"
)

func validateLanguage(value string) (string, *managementResponse) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return languageChinese, nil
	}
	if value != languageChinese && value != languageEnglish {
		r := jsonError(http.StatusBadRequest, "语言必须是 zh 或 en")
		return "", &r
	}
	return value, nil
}
