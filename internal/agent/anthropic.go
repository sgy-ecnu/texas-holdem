package agent

import (
	"encoding/json"
	"errors"
)

func anthropicResponse(data []byte) (string, *int64, *int64, error) {
	var resp struct {
		Content []struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
		} `json:"content"`
		Usage struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	if rejectDuplicateKeys(data) != nil || json.Unmarshal(data, &resp) != nil || len(resp.Content) != 1 || resp.Content[0].Type != "text" || resp.Content[0].Text == nil || !validUsage(resp.Usage.Input, resp.Usage.Output) || len(resp.Error) != 0 {
		return "", nil, nil, errors.New("MODEL_PROTOCOL_ERROR")
	}
	return *resp.Content[0].Text, resp.Usage.Input, resp.Usage.Output, nil
}
