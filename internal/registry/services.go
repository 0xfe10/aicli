package registry

type Service struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Command string `json:"command"`
}

func Services() []Service {
	return []Service{
		{
			ID:      "pingcode",
			Name:    "PingCode",
			Status:  "active",
			Command: "pingcode",
		},
	}
}
