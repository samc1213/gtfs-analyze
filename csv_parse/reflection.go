package csv_parse

import (
	"errors"
	"reflect"
	"strings"
)

type decodeInfo struct {
	fields []fieldDecodeInfo
}

type fieldDecodeInfo struct {
	csvName      string
	timeLayout   string
	defaultValue string
}

// GetDecodeInfo reads the reflection tags
func GetDecodeInfo(t reflect.Type) (decodeInfo, error) {
	var result decodeInfo
	result.fields = make([]fieldDecodeInfo, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		fieldTag := t.Field(i).Tag.Get("csv_parse")
		if fieldTag != "" {
			fieldTag = strings.Trim(fieldTag, " ")
			if !strings.Contains(fieldTag, ";") {
				result.fields[i] = fieldDecodeInfo{csvName: fieldTag}
			} else {
				var csvName string
				var timeLayout string
				var defaultValue string
				splits := strings.Split(fieldTag, ";")
				for _, split := range splits {
					if !strings.Contains(split, ":") {
						if csvName != "" {
							return result, errors.New("Ambiguous csv column name. Possibly " + csvName + " or " + split)
						}
						csvName = split
					} else {
						subKeySplit := strings.Split(split, ":")
						if len(subKeySplit) < 2 {
							return result, errors.New("Invalid csv_parse tag: " + fieldTag)
						}
						subKeyName := subKeySplit[0]
						switch subKeyName {
						case "timeLayout":
							timeLayout = strings.Trim(strings.Join(subKeySplit[1:], ":"), " ")
						case "default":
							defaultValue = strings.Trim(strings.Join(subKeySplit[1:], ":"), " ")
						}
					}
				}

				result.fields[i] = fieldDecodeInfo{csvName: csvName, timeLayout: timeLayout, defaultValue: defaultValue}
			}
		}
	}

	return result, nil
}

type TypeFromCsvConverter interface {
	ConvertFromCsv(string) error
}
