package runner

import "fmt"

type validatedStoreConfigs struct {
	names      map[string]struct{}
	capacities map[string]int
}

func validateStoreConfigs(configs []StoreConfig) (validatedStoreConfigs, error) {
	if len(configs) > MaxStores {
		return validatedStoreConfigs{}, fmt.Errorf("stores must contain at most %d entries", MaxStores)
	}

	validated := validatedStoreConfigs{
		names:      make(map[string]struct{}, len(configs)),
		capacities: make(map[string]int, len(configs)),
	}
	for i, config := range configs {
		if err := validateName(config.Name); err != nil {
			return validatedStoreConfigs{}, fmt.Errorf("stores[%d].name: %w", i, err)
		}
		if _, exists := validated.names[config.Name]; exists {
			return validatedStoreConfigs{}, fmt.Errorf("stores[%d].name %q is duplicated", i, config.Name)
		}
		if config.Capacity < 1 || config.Capacity > MaxStoreCapacity {
			return validatedStoreConfigs{}, fmt.Errorf(
				"stores[%d].capacity must be between 1 and %d",
				i,
				MaxStoreCapacity,
			)
		}
		validated.names[config.Name] = struct{}{}
		validated.capacities[config.Name] = config.Capacity
	}
	return validated, nil
}
