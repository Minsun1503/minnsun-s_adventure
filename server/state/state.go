package state

import "server/models"

// WorldPlayer is our isolated master registry ledger in RAM.
// This is exactly "where to save" our online player connections.
var WorldPlayer = make(map[string]*models.Player)
var MonsterRegistry = make(map[int]*models.Monster)
