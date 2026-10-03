package agent

import (
	"encoding/json"
	"errors"
)

func openAIResponse(data []byte) (string, *int64, *int64, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Input  *int64 `json:"prompt_tokens"`
			Output *int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	if rejectDuplicateKeys(data) != nil || json.Unmarshal(data, &resp) != nil || len(resp.Choices) != 1 || resp.Choices[0].Message.Content == nil || !validUsage(resp.Usage.Input, resp.Usage.Output) || len(resp.Error) != 0 {
		return "", nil, nil, errors.New("MODEL_PROTOCOL_ERROR")
	}
	return *resp.Choices[0].Message.Content, resp.Usage.Input, resp.Usage.Output, nil
}
