package ecs

// EquipmentComponent stores what item template tokens are currently 
// slotted onto the player's active active equipment sockets.
type EquipmentComponent struct {
	WeaponID uint64 // Template ID of equipped weapon (0 = Bare Hands)
	ArmorID  uint64 // Template ID of equipped body armor (0 = Naked)
}
