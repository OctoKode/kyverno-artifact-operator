package gc

// PolicyInfo holds basic policy information
type PolicyInfo struct {
	Name      string
	Namespace string
	Kind      string
	Labels    map[string]string
}
