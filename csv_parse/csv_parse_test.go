package csv_parse

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type TestType struct {
	Field1 string `csv_parse:"field_1"`
	Field2 int    `csv_parse:"field_2"`
}

func TestParseSingleDataRowCsv(t *testing.T) {
	recordProvider, err := BeginParseCsv[TestType](strings.NewReader("field_1,field_2\nvalue_1,2"))
	if err != nil {
		t.Error(err)
	}
	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, "value_1", newRecord.Field1)
	assert.Equal(t, 2, newRecord.Field2)

	newRecord, err = recordProvider.FetchNext()
	// Assert we get EOF back since file is only one data row
	assert.Equal(t, EOF, err)
	assert.Equal(t, "", newRecord.Field1)
	assert.Equal(t, 0, newRecord.Field2)
}

func TestExtraDataColumnCsv(t *testing.T) {
	recordProvider, err := BeginParseCsv[TestType](strings.NewReader("field_1,field_2\nvalue_1,2,extra"))
	if err != nil {
		t.Error(err)
	}
	_, err = recordProvider.FetchNext()
	// This error comes from CSV library, but ensuring it bubbles up here correctly
	assert.Equal(t, "record on line 2: wrong number of fields", err.Error())
}

type UnlabeledFieldType struct {
	Field1         string `csv_parse:"field_1"`
	Field2         int    `csv_parse:"field_2"`
	UnlabeledField []byte
}

func TestIgnoresUnlabeledField(t *testing.T) {
	recordProvider, err := BeginParseCsv[UnlabeledFieldType](strings.NewReader("field_1,field_2\nvalue_1,2"))
	if err != nil {
		t.Error(err)
	}
	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, "value_1", newRecord.Field1)
	assert.Equal(t, 2, newRecord.Field2)
	assert.Equal(t, []byte(nil), newRecord.UnlabeledField)

	newRecord, err = recordProvider.FetchNext()
	// Assert we get EOF back since file is only one data row
	assert.Equal(t, EOF, err)
	assert.Equal(t, "", newRecord.Field1)
	assert.Equal(t, 0, newRecord.Field2)
}

type UnparseableFieldType struct {
	Field1           string `csv_parse:"field_1"`
	Field2           int    `csv_parse:"field_2"`
	UnparseableField []byte `csv_parse:"unparseable_field"`
}

func TestFailsOnUnparseableField(t *testing.T) {
	recordProvider, err := BeginParseCsv[UnparseableFieldType](strings.NewReader("field_1,field_2,unparseable_field\nvalue_1,2,garbage"))
	if err != nil {
		t.Error(err)
	}
	_, err = recordProvider.FetchNext()
	assert.Equal(t, "Cannot convert value 'garbage' to output type []uint8", err.Error())
}

func TestEmptyCsv(t *testing.T) {
	recordProvider, err := BeginParseCsv[UnparseableFieldType](strings.NewReader(""))
	assert.Equal(t, "EOF", err.Error())
	assert.Equal(t, (*RecordProvider[UnparseableFieldType])(nil), recordProvider)
}

type AllType struct {
	StringField  string  `csv_parse:"stringfield"`
	BoolField    bool    `csv_parse:"boolfield"`
	IntField     int     `csv_parse:"intfield"`
	Int8Field    int8    `csv_parse:"int8field"`
	Int16Field   int16   `csv_parse:"int16field"`
	Int32Field   int32   `csv_parse:"int32field"`
	Int64Field   int64   `csv_parse:"int64field"`
	Float32Field float32 `csv_parse:"float32field"`
	Float64Field float64 `csv_parse:"float64field"`
}

func TestAllTypes(t *testing.T) {
	recordProvider, err := BeginParseCsv[TestType](strings.NewReader("stringfield,boolfield,intfield,int8field,int16field,int32field,int64field,float32field,float64field\n1,1,1,1,1,1,1,1,1"))
	if err != nil {
		t.Error(err)
	}
	_, err = recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
}

type DerivedFieldType int32

type DerivedType struct {
	DerivedField DerivedFieldType `csv_parse:"derived"`
}

func TestDerivedType(t *testing.T) {
	recordProvider, err := BeginParseCsv[TestType](strings.NewReader("derived\n233"))
	if err != nil {
		t.Error(err)
	}
	_, err = recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
}

func TestIgnoreTerminatingNewLine(t *testing.T) {
	recordProvider, err := BeginParseCsv[TestType](strings.NewReader("field_1,field_2\nvalue_1,2\n"))
	if err != nil {
		t.Error(err)
	}
	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, "value_1", newRecord.Field1)
	assert.Equal(t, 2, newRecord.Field2)

	newRecord, err = recordProvider.FetchNext()
	// Assert we get EOF back since file is only one data row
	assert.Equal(t, EOF, err)
	assert.Equal(t, "", newRecord.Field1)
	assert.Equal(t, 0, newRecord.Field2)
}

type TimeType struct {
	Field1 time.Time `csv_parse:"field_1;timeLayout:15:04:05"`
}

func TestTime(t *testing.T) {
	recordProvider, err := BeginParseCsv[TimeType](strings.NewReader("field_1\n05:01:55"))
	if err != nil {
		t.Error(err)
	}
	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, 5, newRecord.Field1.Hour())
	assert.Equal(t, 1, newRecord.Field1.Minute())
	assert.Equal(t, 55, newRecord.Field1.Second())
}

type LetterAsNumberType int

func (custom *LetterAsNumberType) ConvertFromCsv(input string) error {
	switch input {
	case "a":
		*custom = 1
		return nil
	case "b":
		*custom = 2
	}
	return errors.New("can only handle a or b")
}

type CustomParseType struct {
	Field1 LetterAsNumberType `csv_parse:"field_1"`
}

func TestCustomParse(t *testing.T) {
	recordProvider, err := BeginParseCsv[CustomParseType](strings.NewReader("field_1\na"))
	if err != nil {
		t.Error(err)
	}
	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, LetterAsNumberType(1), newRecord.Field1)
}
