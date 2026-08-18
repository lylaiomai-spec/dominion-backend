package Services

import (
	"database/sql"
	"encoding/json"
	"os"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var I18nBundle *i18n.Bundle

func InitI18n(localesDir string) error {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			bundle.MustLoadMessageFile(localesDir + "/" + entry.Name())
		}
	}

	I18nBundle = bundle
	return nil
}

func GetUserLanguage(userID int, db *sql.DB) string {
	if userID == 0 {
		return "en"
	}
	var lang sql.NullString
	if err := db.QueryRow("SELECT interface_language FROM users WHERE id = ?", userID).Scan(&lang); err != nil || !lang.Valid || lang.String == "" {
		return "en"
	}
	return lang.String
}

func NewLocalizer(lang string) *i18n.Localizer {
	return i18n.NewLocalizer(I18nBundle, lang, "en")
}

func T(localizer *i18n.Localizer, messageID string) string {
	msg, err := localizer.Localize(&i18n.LocalizeConfig{MessageID: messageID})
	if err != nil {
		return messageID
	}
	return msg
}

func TData(localizer *i18n.Localizer, messageID string, data map[string]interface{}) string {
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})
	if err != nil {
		return messageID
	}
	return msg
}

func TPlural(localizer *i18n.Localizer, messageID string, count int, data map[string]interface{}) string {
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		PluralCount:  count,
		TemplateData: data,
	})
	if err != nil {
		return messageID
	}
	return msg
}
