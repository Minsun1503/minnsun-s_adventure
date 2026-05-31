package main

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

var MonsterRegistry = make(map[int]*Monster)

func LoadMonster() error {
	fileBytes, err := os.ReadFile("monster_template.json")
	if err != nil {
		return fmt.Errorf("failed to read json file: %v", err)
	}

	var temporaryList []Monster

	err = json.Unmarshal(fileBytes, &temporaryList)
	if err != nil {
		return fmt.Errorf("failed to parse json data: %v", err)
	}

	for i := 0; i < len(temporaryList); i++ {
		template := &temporaryList[i]
		MonsterRegistry[template.ID] = template
	}

	fmt.Printf("[Engine] Successfully loaded %d monster templates into memory.\n", len(MonsterRegistry))
	return nil
}
