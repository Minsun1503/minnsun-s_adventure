# Minnsun's Adventure — Binary Protocol Specification (Frozen)

> **Status:** Frozen (Giai đoạn 0 hoàn tất)
> **Byte Order:** Big Endian cho mọi kiểu số nhiều byte (uint16, int32, uint32, uint64)
> **Framing Format:** `[Length uint16 BE] [Opcode uint8] [Payload N-bytes]`
>   - `Length` = kích thước của Opcode **(1 byte)** + kích thước của Payload **(N bytes)**
>   - `MaxPacketSize` = 4096 bytes (DoS protection)

---

## 1. Client → Server (C2S) Packets

| Opcode | Tên | Payload Size | Mô tả |
|--------|-----|-------------|-------|
| 1 | `Move` | 8 bytes | Di chuyển |
| 2 | `Inv` | — | Inventory |
| 3 | `Use` | — | Sử dụng item |
| 4 | `Warp` | — | Dịch chuyển |
| 5 | `Attack` | 8 bytes | Tấn công |
| 6 | `Info` | — | Thông tin |
| 7 | `Quit` | — | Thoát |
| 8 | `Pickup` | — | Nhặt item |
| 9 | `Equip` | — | Trang bị |
| 10 | `Login` | Biến đổi | Đăng nhập |
| 11 | `Register` | Biến đổi | Đăng ký |
| 12 | `PartyCreate` | — | Tạo party |
| 13 | `PartyInvite` | — | Mời party |
| 14 | `PartyJoin` | — | Vào party |
| 15 | `TradeInit` | — | Khởi tạo trade |
| 16 | `TradeOffer` | — | Đề nghị trade |
| 17 | `TradeConfirm` | — | Xác nhận trade |
| 18 | `TradeCancel` | — | Hủy trade |
| 19 | `SkillCast` | — | Dùng kỹ năng |
| 20 | `Chat` | N bytes | Chat |
| 21 | `Heartbeat` | 0 bytes | Giữ kết nối |

### 1.1 Move (Opcode: 1)

```
Payload: [X int32 BE (4B)] [Z int32 BE (4B)]
Total:   8 bytes
```

### 1.2 Attack (Opcode: 5)

```
Payload: [TargetID uint64 BE (8B)]
Total:   8 bytes
```

### 1.3 Chat (Opcode: 20)

```
Payload: [Message UTF-8 string (N bytes)]
Total:   N bytes (biến đổi)
```

### 1.4 Heartbeat (Opcode: 21)

```
Payload: (empty)
Total:   0 bytes
```

### 1.5 Login (Opcode: 10)

```
Payload: [UsernameLen uint8 (1B)] [Username UTF-8 (N bytes)] [PasswordLen uint8 (1B)] [Password UTF-8 (M bytes)]
Total:   2 + N + M bytes
```

### 1.6 Register (Opcode: 11)

```
Payload: [UsernameLen uint8 (1B)] [Username UTF-8 (N bytes)] [PasswordLen uint8 (1B)] [Password UTF-8 (M bytes)]
Total:   2 + N + M bytes
```

---

## 2. Server → Client (S2C) Packets

| Opcode | Tên | Payload Size | Mô tả |
|--------|-----|-------------|-------|
| 0x01 (1) | `Success` | 10 + N bytes | Xác nhận đăng nhập thành công + EntityID |
| 0x10 (16) | `SpawnEntity` | 22 + N bytes | Sinh entity |
| 0x11 (17) | `DespawnEntity` | 8 bytes | Xóa entity |
| 0x12 (18) | `PositionSync` | 16 bytes | Đồng bộ vị trí |
| 0x13 (19) | `StatsSync` | 56 bytes | Đồng bộ chỉ số |
| 0x14 (20) | `CombatHit` | 25 bytes | Chiến đấu |
| 0x15 (21) | `Chat` | 4 + N + M bytes | Tin nhắn chat |
| 0x16 (22) | `Notice` | N bytes | Thông báo hệ thống |
| 0x17 (23) | `Heartbeat` / `Pong` | 0 bytes | Phản hồi heartbeat |
| 0xFF (255) | `Error` | 4 + N bytes | Lỗi |

### 2.1 Success (Opcode: 0x01)

```
Payload: [EntityID uint64 BE (8B)] [MessageLen uint16 BE (2B)] [Message UTF-8 (N bytes)]
Total:   10 + N bytes
```

### 2.2 SpawnEntity (Opcode: 0x10)

```
Payload: [EntityID uint64 BE (8B)] [Type uint8 (1B)] [MapID int32 BE (4B)] [X int32 BE (4B)] [Z int32 BE (4B)] [NameLen uint8 (1B)] [Name UTF-8 (N bytes)]
Total:   22 + N bytes

Type values: 0 = player, 1 = monster, 2 = ground_item
```

### 2.3 DespawnEntity (Opcode: 0x11)

```
Payload: [EntityID uint64 BE (8B)]
Total:   8 bytes
```

### 2.4 PositionSync (Opcode: 0x12)

```
Payload: [EntityID uint64 BE (8B)] [X int32 BE (4B)] [Z int32 BE (4B)]
Total:   16 bytes
```

### 2.5 StatsSync (Opcode: 0x13)

```
Payload: [EntityID uint64 BE (8B)]
         [HP:MaxHP packed uint64 BE (8B)]       — high 32b = HP, low 32b = MaxHP
         [MP:MaxMP packed uint64 BE (8B)]       — high 32b = MP, low 32b = MaxMP
         [Dam:Level packed uint64 BE (8B)]      — high 32b = Dam, low 32b = Level
         [Defense:MagicDefense packed uint64 BE (8B)]  — high 32b = Defense, low 32b = MagicDefense
         [MagicAttack:HitRate packed uint64 BE (8B)]   — high 32b = MagicAttack, low 32b = HitRate
         [DodgeRate:CritRate packed uint64 BE (8B)]    — high 32b = DodgeRate, low 32b = CritRate
Total:   8 + (6 × 8) = 56 bytes
```

### 2.6 CombatHit (Opcode: 0x14)

```
Payload: [AttackerID uint64 BE (8B)] [TargetID uint64 BE (8B)] [Damage int32 BE (4B)] [TargetHP int32 BE (4B)] [Killed uint8 (1B)]
Total:   25 bytes
```

### 2.7 Chat (Opcode: 0x15)

```
Payload: [Channel uint8 (1B)] [SenderNameLen uint8 (1B)] [SenderName UTF-8 (N bytes)] [MessageLen uint16 BE (2B)] [Message UTF-8 (M bytes)]
Total:   4 + N + M bytes
```

### 2.8 Notice (Opcode: 0x16)

```
Payload: [Message UTF-8 string (N bytes)]
Total:   N bytes (biến đổi)
```

### 2.9 Heartbeat / Pong (Opcode: 0x17)

```
Payload: (empty)
Total:   0 bytes
```

### 2.10 Error (Opcode: 0xFF)

```
Payload: [ErrorCode uint16 BE (2B)] [MessageLen uint16 BE (2B)] [Message UTF-8 (N bytes)]
Total:   4 + N bytes
```

---

## 3. Error Codes

| Code | Tên | Mô tả |
|------|-----|-------|
| 1 | `ErrCodeServerFull` | Server đầy, không thể login |
| 2 | `ErrCodeDatabaseError` | Lỗi database |
| 3 | `ErrCodeInternalError` | Lỗi nội bộ (generic) |

---

## 4. Framing Rules

- Mọi gói tin đều bắt đầu bằng `[Length uint16 BE]` (2 bytes).
- `Length` = 1 (opcode) + len(payload).
- Opcode chiếm đúng **1 byte** ngay sau Length.
- Payload là phần còn lại của gói tin.
- **Client implementation (C# Unity):** `NetworkClient.cs` và `NetworkClientWS.cs` parse đúng framing này.
- **Server implementation (Go):** `peakgo/broadcast/broadcast.go` xây dựng S2C packet; `netio/packet.go` đọc/ghi framing.

---

## 5. Connection Lifecycle

```
Client                    Server
  │                         │
  ├─ [TCP Connect] ────────►│
  │                         │  processLogin()
  │                         │
  ├─ [Login/Register] ─────►│
  │                         ├─ [Success with EntityID] ──► Client
  │                         ├─ [SpawnEntity (self)] ─────► Client
  │                         ├─ [StatsSync (self)] ───────► Client
  │                         │  HandleClient() loop:
  │  ┌──────────────────────┼──────────────────────────┐
  │  │ [Move/Attack/Chat] ──►│                          │
  │  │                      ├─ [PositionSync] ─────────► Neighbors
  │  │                      ├─ [CombatHit] ────────────► Neighbors
  │  │                      └─ ...                      │
  │  └──────────────────────┼──────────────────────────┘
  │                         │
  ├─ [Heartbeat] ──────────►│
  │                         ├─ [Pong (0x17, empty)] ───► Client
  │                         │
  │       ... idle 45s ...   │
  │                         │  (read timeout → disconnect)
```

---

## 6. Implementation References

| Component | File | Vai trò |
|-----------|------|---------|
| Server S2C Builders | `server/peakgo/broadcast/broadcast.go` | Xây dựng tất cả S2C packet |
| Server S2C Tests | `server/peakgo/broadcast/broadcast_test.go` | Layout + zero-alloc tests |
| Server C2S Opcodes | `server/protocol/opcodes.go` | Định nghĩa opcode C2S và S2C |
| Server C2S Handlers | `server/network/handler_registry.go` | Đăng ký handler cho từng opcode C2S |
| Server Auth | `server/auth/auth.go` | Xử lý Login/Register packets |
| Server Router | `server/network/router.go` | Parse framing + dispatch |
| Server Packet I/O | `server/peakgo/netio/packet.go` | ReadHeader/ReadPayload/WritePacket |
| Client S2C Opcodes | `Assets/Scripts/Opcodes.cs` | Định nghĩa opcode C# |
| Client S2C Decoders | `Assets/Scripts/Decoders.cs` | Decode payload → typed structs |
| Client Packet Router | `Assets/Scripts/PacketRouter.cs` | Route opcode → handler |
| Client TCP (Editor) | `Assets/Scripts/NetworkClient.cs` | TCP client với binary framing |
| Client WS (WebGL) | `Assets/Scripts/NetworkClientWS.cs` | WebSocket client với binary framing |