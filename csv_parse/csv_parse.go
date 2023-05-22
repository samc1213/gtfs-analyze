package csv_parse

import (
	"encoding/csv"
	"errors"
	"io"
	"reflect"
	"strconv"
	"time"
)

var EOF = errors.New("EOF")

type RecordProvider[T any] struct {
	columnNameToIdx map[string]int
	reader          *csv.Reader
	recordType      reflect.Type
	decodeInfo      decodeInfo
}

func newRecordProvider[T any](columnNameToIdx map[string]int, reader *csv.Reader) (*RecordProvider[T], error) {
	var t T
	recordType := reflect.TypeOf(t)

	decodeInfo, err := GetDecodeInfo(recordType)
	return &RecordProvider[T]{columnNameToIdx: columnNameToIdx, reader: reader, recordType: recordType, decodeInfo: decodeInfo}, err
}

func convertValueToType(value string, outputType reflect.Type, timeLayout string) (any, error) {
	if reflect.PointerTo(outputType).Implements(reflect.TypeOf(new(TypeFromCsvConverter)).Elem()) {
		newVal := reflect.New(outputType)
		parsed := newVal.Interface()
		val := parsed.(TypeFromCsvConverter)
		err := val.ConvertFromCsv(value)
		return newVal.Elem().Interface(), err
	}
	switch outputType {
	case reflect.TypeOf(time.Time{}):
		if timeLayout == "" {
			return time.Time{}, errors.New("must specify a timeLayout when parsing to time.Time")
		} else {
			return time.Parse(timeLayout, value)
		}
	}

	switch outputType.Kind() {
	case reflect.Int:
		parsed, err := strconv.ParseInt(value, 10, 32)
		return int(parsed), err
	case reflect.Int8:
		parsed, err := strconv.ParseInt(value, 10, 8)
		return int8(parsed), err
	case reflect.Int16:
		parsed, err := strconv.ParseInt(value, 10, 16)
		return int16(parsed), err
	case reflect.Int32:
		parsed, err := strconv.ParseInt(value, 10, 32)
		return int32(parsed), err
	case reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return int64(parsed), err
	case reflect.Bool:
		return strconv.ParseBool(value)
	case reflect.Float32:
		value, err := strconv.ParseFloat(value, 32)
		return float32(value), err
	case reflect.Float64:
		return strconv.ParseFloat(value, 64)
	case reflect.String:
		return value, nil
	}
	return nil, errors.New("Cannot convert value '" + value + "' to output type " + outputType.String())
}

func (r *RecordProvider[T]) FetchNext() (T, error) {
	record, err := r.reader.Read()
	var parsedRecord T
	if err == io.EOF {
		return parsedRecord, EOF
	}
	if err != nil {
		return parsedRecord, err
	}
	for i, fieldDecodeInfo := range r.decodeInfo.fields {
		columnName := fieldDecodeInfo.csvName
		columnIdx, found := r.columnNameToIdx[columnName]
		if found {
			csvValue := record[columnIdx]
			if csvValue == "" && fieldDecodeInfo.defaultValue != "" {
				csvValue = fieldDecodeInfo.defaultValue
			}
			convertedValue, err := convertValueToType(csvValue, r.recordType.Field(i).Type, fieldDecodeInfo.timeLayout)
			if err != nil {
				return parsedRecord, err
			}
			field := reflect.ValueOf(&parsedRecord).Elem().Field(i)
			// Convert to field.Type in case convertedValue is of a base type and needs to be downcast (e.g., Enum defined from int8 base)
			field.Set(reflect.ValueOf(convertedValue).Convert(field.Type()))
		}
	}
	return parsedRecord, nil
}

func BeginParseCsv[T any](input io.Reader) (*RecordProvider[T], error) {
	reader := csv.NewReader(input)
	header, err := reader.Read()
	if err != nil && err != io.EOF {
		print("oopsies")
		return nil, err
	}
	// Empty file
	if len(header) == 0 {
		return nil, EOF
	}
	columnNameToIdx := make(map[string]int)
	for i, columnName := range header {
		columnNameToIdx[columnName] = i
	}
	return newRecordProvider[T](columnNameToIdx, reader)
}
