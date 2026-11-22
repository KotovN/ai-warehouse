package config

// ResourceTypes are the asset categories the hub repo holds and aiwarehouse syncs.
var ResourceTypes = []string{"skills", "mcp", "clis", "hooks", "rules", "docs"}

// IsResourceType reports whether name is a known resource type.
func IsResourceType(name string) bool {
	for _, t := range ResourceTypes {
		if t == name {
			return true
		}
	}
	return false
}
