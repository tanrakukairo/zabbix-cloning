package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type Object map[string]any

type StoreItem struct {
	DataID string `json:"DATA_ID,omitempty"`
	Name   string `json:"NAME"`
	Data   Object `json:"DATA"`
}

type Dataset map[string][]StoreItem

type Version struct {
	VersionID     string  `json:"VERSION_ID" dynamodbav:"VERSION_ID"`
	UnixTime      int64   `json:"UNIXTIME" dynamodbav:"UNIXTIME"`
	MasterVersion float64 `json:"MASTER_VERSION" dynamodbav:"MASTER_VERSION"`
	Description   string  `json:"DESCRIPTION" dynamodbav:"DESCRIPTION"`
}

type Record struct {
	VersionID string `json:"VERSION_ID" dynamodbav:"VERSION_ID"`
	DataID    string `json:"DATA_ID" dynamodbav:"DATA_ID"`
	Method    string `json:"METHOD" dynamodbav:"METHOD"`
	Name      string `json:"NAME" dynamodbav:"NAME"`
	Data      Object `json:"DATA" dynamodbav:"-"`
	Payload   []byte `json:"-" dynamodbav:"DATA"`
}

func (d Dataset) Records(versionID string) []Record {
	methods := make([]string, 0, len(d))
	for method := range d {
		methods = append(methods, method)
	}
	sort.Strings(methods)

	var records []Record
	for _, method := range methods {
		for _, item := range d[method] {
			records = append(records, Record{
				VersionID: versionID,
				DataID:    item.DataID,
				Method:    method,
				Name:      item.Name,
				Data:      item.Data,
			})
		}
	}
	return records
}

func DatasetFromRecords(records []Record) Dataset {
	dataset := Dataset{}
	for _, record := range records {
		dataset[record.Method] = append(dataset[record.Method], StoreItem{
			DataID: record.DataID,
			Name:   record.Name,
			Data:   record.Data,
		})
	}
	return dataset
}

func CloneObject(value Object) Object {
	data, _ := json.Marshal(value)
	var cloned Object
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func String(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func Int(value any) int {
	n, _ := strconv.Atoi(String(value))
	return n
}

func Float(value any) float64 {
	n, _ := strconv.ParseFloat(String(value), 64)
	return n
}

func Bool(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
		return v == "YES" || v == "yes"
	default:
		return Int(v) != 0
	}
}

func NowUnix() int64 { return time.Now().UTC().Unix() }

func ZabbixTime() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }
