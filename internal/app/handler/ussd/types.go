package ussd

type ExecuteRequest struct {
	Action string `json:"action" validate:"required,oneof=initialize reply"`
	Code   string `json:"code" validate:"required"`
}

type ExecuteResponse struct {
	Reply string `json:"reply" jsonschema:"text returned by the carrier USSD session"`
}
