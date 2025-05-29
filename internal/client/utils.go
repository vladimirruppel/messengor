package client

import (
	"errors"
	"fmt"
	"sort"
)

func GenerateClientPrivateChatID(userID1, userID2 string) (string, error) {
	if userID1 == "" || userID2 == "" {
		return "", errors.New("user IDs cannot be empty for generating chat ID")
	}
	ids := []string{userID1, userID2}
	sort.Strings(ids)
	return fmt.Sprintf("private:%s:%s", ids[0], ids[1]), nil
}
