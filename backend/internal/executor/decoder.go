package executor

import (
	"fmt"

	ycrdt "github.com/skyterra/y-crdt"
)

func DecodeYjsToText(binaryState []byte) (string, error) {
	if len(binaryState) == 0 {
		return "", nil
	}

	doc := ycrdt.NewDoc("", true, ycrdt.DefaultGCFilter, nil, false)
	ycrdt.ApplyUpdate(doc, binaryState, nil)

	ytext := doc.GetText("content")
	if ytext == nil {
		return "", fmt.Errorf("no text type found under key 'content'")
	}

	return ytext.ToString(), nil
}
