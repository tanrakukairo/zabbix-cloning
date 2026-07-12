package store

import (
	"bytes"
	stdbzip2 "compress/bzip2"
	"encoding/json"
	"fmt"
	"io"

	dsbzip2 "github.com/dsnet/compress/bzip2"
)

func compressJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := dsbzip2.NewWriter(&buffer, &dsbzip2.WriterConfig{})
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decompressJSON(data []byte, target any) error {
	reader := stdbzip2.NewReader(bytes.NewReader(data))
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if err == io.EOF {
			return fmt.Errorf("empty bzip2 JSON payload")
		}
		return err
	}
	return nil
}
