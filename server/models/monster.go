package models

import (
	"encoding/json"
	"fmt"
	"os"
)

type Monster struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	HP   int    `json:"hp"`
	Dam  int    `json:"damage"`
}

func LoadMonster(filePath string) ([]Monster, error) {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read json file: %v", err)
	}

	var temporaryList []Monster

	err = json.Unmarshal(fileBytes, &temporaryList)
	if err != nil {
		return nil, fmt.Errorf("failed to parse json data: %v", err)
	}

	return temporaryList, nil
}
