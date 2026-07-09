package jsonrpc

import "encoding/json"

const Version = "2.0"

type ID string

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewRequest(id ID, method string, params any) (Request, error) {
	raw, err := marshalOptional(params)
	if err != nil {
		return Request{}, err
	}
	return Request{JSONRPC: Version, ID: id, Method: method, Params: raw}, nil
}

func NewNotification(method string, params any) (Notification, error) {
	raw, err := marshalOptional(params)
	if err != nil {
		return Notification{}, err
	}
	return Notification{JSONRPC: Version, Method: method, Params: raw}, nil
}

func NewResult(id ID, result any) (Response, error) {
	raw, err := marshalOptional(result)
	if err != nil {
		return Response{}, err
	}
	return Response{JSONRPC: Version, ID: id, Result: raw}, nil
}

func NewError(id ID, code int, message string) Response {
	return Response{JSONRPC: Version, ID: id, Error: &Error{Code: code, Message: message}}
}

func marshalOptional(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
