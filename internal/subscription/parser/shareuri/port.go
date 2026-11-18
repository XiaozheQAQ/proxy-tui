package shareuri

import "strconv"

func parsePort(value string) int {
	if value == "" {
		return 0
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}
