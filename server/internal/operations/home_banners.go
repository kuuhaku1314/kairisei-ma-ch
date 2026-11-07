package operations

import "encoding/json"

// HomeBannerPolicy is a global presentation policy, never player save data.
// Custom=false retains the existing system recommendations; Custom=true uses
// exactly the enabled, scheduled rows, including an intentionally empty list.
type HomeBannerPolicy struct {
	Custom bool              `json:"custom"`
	Items  []HomeBannerEntry `json:"items"`
}

// Forms and publication documents use an array for an empty ordered list.
func (policy HomeBannerPolicy) MarshalJSON() ([]byte, error) {
	type wire HomeBannerPolicy
	value := wire(policy)
	if value.Items == nil {
		value.Items = []HomeBannerEntry{}
	}
	return json.Marshal(value)
}

type HomeBannerEntry struct {
	Title     string `json:"title"`
	Enabled   bool   `json:"enabled"`
	CoverPath string `json:"cover_path"`
	Target    string `json:"target"`
	TargetID  int    `json:"target_id,omitempty"`
	StartUnix int64  `json:"start_unix,omitempty"`
	EndUnix   int64  `json:"end_unix,omitempty"`
}

type HomeBannerConfiguration struct {
	Policy       HomeBannerPolicy
	ImageBaseURL string
}

type HomeBannerConfigurator interface {
	ApplyHomeBannerConfiguration(HomeBannerConfiguration)
}
