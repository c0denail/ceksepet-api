package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"ceksepet-api/internal/security"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) *apiError {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var syntaxError *json.SyntaxError
		var typeError *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return validationError("body", "İstek gövdesi boş olamaz.")
		case errors.As(err, &syntaxError):
			return validationError("body", "JSON biçimi geçersiz.")
		case errors.As(err, &typeError):
			return validationError(typeError.Field, "Alan tipi geçersiz.")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.Trim(err.Error()[len("json: unknown field "):], `"`)
			return validationError(field, "Bilinmeyen alan.")
		case err.Error() == "http: request body too large":
			return validationError("body", "İstek gövdesi çok büyük.")
		default:
			return validationError("body", "İstek gövdesi okunamadı.")
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return validationError("body", "Yalnızca tek bir JSON nesnesi gönderilebilir.")
	}
	return nil
}

func normalizePhone(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	value = replacer.Replace(value)
	switch {
	case strings.HasPrefix(value, "05") && len(value) == 11:
		value = "+9" + value
	case strings.HasPrefix(value, "5") && len(value) == 10:
		value = "+90" + value
	case strings.HasPrefix(value, "0090"):
		value = "+" + value[2:]
	}
	return value, phonePattern.MatchString(value)
}

func validName(value string) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(value))
	return length >= 1 && length <= 100
}

func validPIN(value string) bool { return security.IsSixDigits(value) }

func usernameBase(firstName, lastName string) string {
	value := strings.ToLower(strings.TrimSpace(firstName) + "_" + strings.TrimSpace(lastName))
	value = strings.NewReplacer(
		"ç", "c", "ğ", "g", "ı", "i", "ö", "o", "ş", "s", "ü", "u",
	).Replace(value)
	var builder strings.Builder
	underscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r <= unicode.MaxASCII {
				builder.WriteRune(r)
			}
			underscore = false
			continue
		}
		if !underscore && builder.Len() > 0 {
			builder.WriteByte('_')
			underscore = true
		}
	}
	base := strings.Trim(builder.String(), "_")
	if base == "" {
		base = "kullanici"
	}
	if len(base) > 40 {
		base = strings.TrimRight(base[:40], "_")
	}
	return base
}

func validationError(field, message string) *apiError {
	return &apiError{
		Code: "VALIDATION_ERROR", Message: "Gönderilen alanları kontrol edin.",
		FieldErrors: map[string]string{field: message},
	}
}

func collectValidation(fields map[string]string) *apiError {
	if len(fields) == 0 {
		return nil
	}
	return &apiError{Code: "VALIDATION_ERROR", Message: "Gönderilen alanları kontrol edin.", FieldErrors: fields}
}
