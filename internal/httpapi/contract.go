package httpapi

// Wire request DTOs are shared with contractgen. Domain services retain their own inputs.
type LoginInput struct {
	Provider string `json:"provider"`
	ReturnTo string `json:"return_to"`
}
type QQInput struct {
	Number string `json:"qq_number"`
}
type CreateOrderInput struct {
	Amount  int64  `json:"amount_fen"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Key     string `json:"idempotency_key"`
}
type IDInput struct {
	ID string `json:"id"`
}
type UnbindIdentityInput struct {
	ID         string `json:"id"`
	IdentityID string `json:"identity_id"`
}
type OperationInput struct {
	ID string `json:"operation_id"`
}
type PreviewInput struct {
	Action string   `json:"action"`
	IDs    []string `json:"ids"`
}
type ReconcileInput struct {
	ID  string `json:"id"`
	Key string `json:"idempotency_key"`
}
