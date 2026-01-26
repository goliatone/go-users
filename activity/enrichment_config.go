package activity

// EnrichmentScope determines when enrichment should apply.
type EnrichmentScope string

const (
	EnrichmentScopeWrite    EnrichmentScope = "write"
	EnrichmentScopeBackfill EnrichmentScope = "backfill"
	EnrichmentScopeBoth     EnrichmentScope = "both"
)

// EnrichmentWriteMode determines how write-time enrichment is applied.
type EnrichmentWriteMode string

const (
	EnrichmentWriteModeNone    EnrichmentWriteMode = "none"
	EnrichmentWriteModeWrapper EnrichmentWriteMode = "wrapper"
	EnrichmentWriteModeRepo    EnrichmentWriteMode = "repo"
	EnrichmentWriteModeHybrid  EnrichmentWriteMode = "hybrid"
)
