package providers

// Reasoning controls the extended thinking / reasoning mode for a completion request.
// Each provider translates these values to its own native API format.
//
// The zero value ("") means no preference — the model's built-in default is used,
// which is functionally identical to ReasoningDefault.
type Reasoning string

const (
	// ReasoningEmpty is the zero value: no explicit preference.
	// The provider uses the model's built-in default, or auto-detects from
	// models.dev metadata (same as ReasoningDefault).
	ReasoningEmpty Reasoning = ""

	// ReasoningDefault explicitly requests the model's default reasoning level.
	// Functionally identical to ReasoningEmpty.
	ReasoningDefault Reasoning = "default"

	// ReasoningAuto lets the provider or model decide whether and how much to
	// reason. For models that support adaptive thinking this maps to that mode.
	ReasoningAuto Reasoning = "auto"

	// ReasoningEnabled enables reasoning with a provider-defined default effort.
	// Kept as a legacy alias; new code should prefer a specific level.
	ReasoningEnabled Reasoning = "enabled"

	// ReasoningDisabled turns off reasoning / extended thinking entirely.
	ReasoningDisabled Reasoning = "disabled"

	// ReasoningNone is an alias for ReasoningDisabled.
	ReasoningNone Reasoning = "none"

	// ReasoningLow requests low-effort reasoning (e.g. small thinking budget or
	// low reasoning_effort). The exact meaning is provider-specific.
	ReasoningLow Reasoning = "low"

	// ReasoningMedium requests medium-effort reasoning.
	ReasoningMedium Reasoning = "medium"

	// ReasoningHigh requests high-effort reasoning.
	ReasoningHigh Reasoning = "high"

	// ReasoningXHigh requests maximum-effort reasoning, beyond the normal "high"
	// level where supported.
	ReasoningXHigh Reasoning = "xhigh"
)
