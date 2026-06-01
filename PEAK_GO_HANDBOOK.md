# Peak Go Handbook — Minnsun's Adventure

Tài liệu này là **coding standard bắt buộc** cho mọi tính năng mới thêm vào server.
Mục tiêu: bất kỳ System nào tuân thủ handbook này sẽ **tự động** kế thừa trạng thái Peak Go mà không cần refactor về sau.

---

## Nhanh: Checklist trước khi merge

Trước khi push code mới, chạy toàn bộ checklist sau:

- [ ] Không có `make([]byte, n)` hoặc `make([]T, 0, n)` trong hot loop (vòng lặp chạy ≥ 4 lần/giây)
- [ ] Không có `binary.Read(...)` hoặc `binary.Write(...)` — dùng `codec.*` thay thế
- [ ] Không có `logger.Debug(...)` trực tiếp — dùng `loggate.Debugf(...)` thay thế
- [ ] Không có `if logger.IsDebug() { logger.Debug(...) }` thủ công — `loggate.Debugf` tự làm điều này
- [ ] Không có `rand.New(rand.NewSource(...))` trong bất kỳ hàm nào — dùng `rng.Intn`, `rng.Float64`
- [ ] Không có `x < 0 || x > 100 || z < 0 || z > 100` inline — dùng `gmath.InBounds(x, z, 0, 100)`
- [ ] Không có `dx*dx + dz*dz` inline trong hot path — dùng `gmath.DistanceSq(ax, az, bx, bz)`
- [ ] Không có chuỗi if/else clamp thủ công — dùng `gmath.Clamp` hoặc `gmath.ClampPos`
- [ ] Mọi Entity ID được định nghĩa là `ecs.Entity` (uint64), không dùng string ID
- [ ] Mọi Component mới đã được đăng ký trong `ecs.Registry` dưới dạng `ComponentStore[T]`
- [ ] Mọi System mới không import ngược (`ecs` không import `systems`, `models` không import `server`)
- [ ] Có benchmark test với `b.ReportAllocs()` cho mọi hot-path function mới

---

## Patterns được phép ✅ vs. Bị cấm ❌

### Không gian và toán học (spatial math)

| ❌ Cấm | ✅ Peak Go |
|---|---|
| `dx*dx + dz*dz` inline trong hot loop | `gmath.DistanceSq(ax, az, bx, bz)` |
| `math.Sqrt(dx*dx+dz*dz) <= radius` | `gmath.InRange(ax, az, bx, bz, radius)` |
| `x < 0 \|\| x > 100 \|\| z < 0 \|\| z > 100` | `!gmath.InBounds(x, z, 0, 100)` |
| 4 dòng if/else clamp thủ công | `gmath.ClampPos(x, z, 0, 100)` |

**Ví dụ range check trong System mới:**
```go
// ❌ Cũ — inline, 2 biến tạm, dễ quên sqrt
dx := float64(a.X - b.X)
dz := float64(a.Z - b.Z)
if dx*dx+dz*dz <= meleeRange*meleeRange { ... }

// ✅ Peak Go — named, readable, 0 allocs
if gmath.InRange(a.X, a.Z, b.X, b.Z, meleeRange) { ... }
```

**Ví dụ loot scatter (clamp sau random offset):**
```go
// ❌ Cũ — 8 dòng if/else
offsetX := rng.Intn(3) - 1
dropX := targetPos.X + offsetX
if dropX < 0 { dropX = 0 }
if dropX > 100 { dropX = 100 }
// ... lặp lại cho Z

// ✅ Peak Go — 3 dòng
offsetX := rng.Intn(3) - 1
offsetZ := rng.Intn(3) - 1
dropX, dropZ := gmath.ClampPos(targetPos.X+offsetX, targetPos.Z+offsetZ, 0, 100)
```

---

### Random Number Generation

| ❌ Cấm (gây heap alloc hoặc mutex contention) | ✅ Peak Go |
|---|---|
| `rand.New(rand.NewSource(time.Now().UnixNano()))` | `rng.Float64()` hoặc `rng.Intn(n)` |
| `rand.Intn(n)` global | `rng.Intn(n)` — pooled, không mutex |
| `rand.Float64()` global | `rng.Float64()` — pooled |
| `lo + rand.Intn(hi-lo)` | `rng.IntnRange(lo, hi)` |

> **Tại sao `rand.New(rand.NewSource(...))` bị cấm?**  
> Mỗi lần gọi tạo một `*rand.Rand` mới trên heap. Khi 50 monster bị kill cùng lúc → 50 allocations trong một game tick. `rng.Float64()` tái sử dụng pool → **0 allocs**.

**Ví dụ probability roll (loot drop):**
```go
// ❌ Cũ — alloc mỗi lần gọi
r := rand.New(rand.NewSource(time.Now().UnixNano()))
if r.Float64() <= dropChance { ... }

// ✅ Peak Go
if rng.Float64() <= dropChance { ... }
```

**Ví dụ random offset:**
```go
// ❌ Cũ — global mutex
dx := rand.Intn(5) - 2

// ✅ Peak Go — pooled
dx := rng.Intn(5) - 2
```

| ❌ Cấm (gây GC pressure) | ✅ Peak Go |
|---|---|
| `binary.Read(conn, binary.BigEndian, &length)` | `netio.ReadHeader(conn)` |
| `make([]byte, length)` để đọc payload | `netio.ReadPayload(conn, netio.DefaultPool, length)` |
| `conn.Write(data)` không có deadline | `netio.WritePacket(conn, data)` |
| Tự tạo `sync.Pool{New: ...}` trong package mới | `pool.NewBytesPool(size)` hoặc `pool.NewSlicePool[T](cap)` |

**Lifecycle bắt buộc với `ReadPayload`:**
```go
pBuf, err := netio.ReadPayload(conn, netio.DefaultPool, length)
if err != nil { ... }
defer netio.DefaultPool.Put(pBuf) // PHẢI put lại sau khi dùng xong
payload := (*pBuf)[:length]
```

### Decode binary packet

| ❌ Cấm | ✅ Peak Go |
|---|---|
| `binary.BigEndian.Uint16(buf)` inline | `codec.ReadUint16(buf)` |
| `binary.BigEndian.Uint32(buf)` inline | `codec.ReadUint32(buf)` |
| `binary.BigEndian.Uint64(buf)` inline | `codec.ReadUint64(buf)` |
| Manual offset arithmetic cho MOVE payload | `codec.ReadMovePayload(payload)` |
| Manual offset arithmetic cho ATTACK payload | `codec.ReadAttackPayload(payload)` |

**Ví dụ MOVE handler:**
```go
// ❌ Cũ — inline, dễ sai offset
targetX := int(int32(binary.BigEndian.Uint32(payload[0:4])))
targetZ := int(int32(binary.BigEndian.Uint32(payload[4:8])))

// ✅ Peak Go — typed, validated, zero-alloc
p, ok := codec.ReadMovePayload(payload)
if !ok {
    return "Error: Invalid MOVE payload\r\n", false
}
MovementSystem(playerID, p.X, p.Z)
```

### Logging

| ❌ Cấm (gây fmt.Sprintf + boxing trong production) | ✅ Peak Go |
|---|---|
| `logger.Debug("entity %d moved", id)` | `loggate.Debugf("entity %d moved", id)` |
| `if logger.IsDebug() { logger.Debug(...) }` thủ công | `loggate.Debugf(...)` — tự built-in guard |
| `logger.Info(...)` | `loggate.Infof(...)` (hoặc `logger.Info` trực tiếp — đều ok) |
| `logger.Warn(...)` | `loggate.Warnf(...)` (hoặc `logger.Warn` trực tiếp — đều ok) |

> **Tại sao?** `logger.Debug("msg", args...)` luôn evaluate `args` trước khi check debug mode → fmt.Sprintf chạy trong production → interface boxing → GC pressure.
> `loggate.Debugf` check `logger.IsDebug()` trước → nếu false, toàn bộ argument evaluation bị skip.

### Memory pooling

| ❌ Cấm | ✅ Peak Go |
|---|---|
| `make([]SomeStruct, 0, 16)` trong game loop | `pool.NewSlicePool[SomeStruct](16)` |
| Type assertion thủ công: `pool.Get().(*[]byte)` | `bytesPool.Get()` — typed, no assertion |
| Quên put lại pool sau khi dùng | `defer pool.Put(ptr)` ngay sau Get |

**Pattern chuẩn cho accumulator slice trong game loop:**
```go
// Khai báo một lần ở package level:
var myResultPool = pool.NewSlicePool[MyResult](16)

// Trong hot path:
ps := myResultPool.Get()
results := *ps
defer myResultPool.Put(ps)

for _, item := range candidates {
    results = append(results, MyResult{...})
}
// Dùng results...
```

### ECS

| ❌ Cấm | ✅ Peak Go |
|---|---|
| String ID cho Entity: `"player_abc"` | `ecs.Entity` (uint64) — O(1) lookup |
| Pointer vào Component: `*PositionComponent` | Value type trực tiếp — tránh extra heap alloc |
| Component chứa logic nghiệp vụ | Component chỉ chứa data thuần; logic ở System |
| Ghi ECS trực tiếp từ AI | AI chỉ ghi `AIComponent` của chính nó; mọi thứ khác qua Systems |
| `sync.Map` raw | `ComponentStore[T]` (Paged Sparse Set) đã tích hợp trong Registry |

**Copy-modify-overwrite pattern (bắt buộc cho reference-type components):**
```go
// ✅ Đúng — nhận value copy, mutate, ghi lại
comp, ok := registry.GetXxx(id)
if !ok { return }
comp.SomeField = newValue
registry.SetXxx(id, comp) // Ghi lại value mới

// ❌ Sai — không dùng pointer đến component bên trong store
```

---

## Cách thêm Component mới (Peak Go compliant)

1. **Định nghĩa struct** trong `server/ecs/ecs.go` — data only, không có method logic:
   ```go
   type NewComponent struct {
       Field1 int
       Field2 string
   }
   ```

2. **Nếu component chứa slice/map** → thêm `Clone()` method để hỗ trợ copy-on-write:
   ```go
   func (c NewComponent) Clone() NewComponent {
       return NewComponent{
           Field1: c.Field1,
           Items:  append([]ItemType(nil), c.Items...),
       }
   }
   ```

3. **Thêm `ComponentStore[T]`** vào `Registry` struct:
   ```go
   type Registry struct {
       // ...existing...
       newComps ComponentStore[NewComponent]
   }
   ```

4. **Thêm helper methods** vào `Registry`:
   ```go
   func (r *Registry) SetNew(id Entity, comp NewComponent) { r.newComps.Set(id, comp) }
   func (r *Registry) GetNew(id Entity) (NewComponent, bool) { return r.newComps.Get(id) }
   func (r *Registry) DeleteNew(id Entity) { r.newComps.Delete(id) }
   ```

5. **Đăng ký trong `RemoveEntity`**:
   ```go
   func (r *Registry) RemoveEntity(id Entity) {
       // ...existing...
       r.newComps.Delete(id)
   }
   ```

---

## Cách thêm Opcode mới (Peak Go compliant)

1. **Khai báo opcode constant** trong `server/systems/opcodes.go`:
   ```go
   const OpcodeC2SNewAction byte = 21
   ```

2. **Nếu là hot-path** (dự kiến gọi nhiều lần/giây) → thêm composite reader vào `server/peakgo/codec/codec.go`:
   ```go
   type NewActionPayload struct { TargetID uint64; Value int32 }
   func ReadNewActionPayload(b []byte) (NewActionPayload, bool) {
       if len(b) != 12 { return NewActionPayload{}, false }
       return NewActionPayload{
           TargetID: ReadUint64(b[0:8]),
           Value:    ReadInt32(b[8:12]),
       }, true
   }
   ```

3. **Thêm case** trong `handleBinaryPacket` trong `server/server.go`:
   ```go
   case protocol.OpcodeC2SNewAction:
       p, ok := codec.ReadNewActionPayload(payload)
       if !ok {
           systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid payload\r\n"))
           return
       }
       game.HandleNewActionSystem(playerEntity, p)
   ```

4. **Thêm System** trong `server/game/`:
   ```go
   // server/game/new_action.go
   package game
   import "server/peakgo/loggate"
   func HandleNewActionSystem(playerID ecs.Entity, p codec.NewActionPayload) {
       loggate.Debugf("[NEW_ACTION] player %d → target %d value %d", playerID, p.TargetID, p.Value)
       // logic...
   }
   ```

5. **Viết benchmark test** trong package tương ứng:
   ```go
   func BenchmarkNewAction(b *testing.B) {
       b.ReportAllocs()
       // ... assert 0 allocs/op
   }
   ```

---

## Câu hỏi thường gặp

**Q: Tôi cần pass dữ liệu giữa hai goroutine, dùng gì?**  
→ Channel có buffer nếu là queue (ví dụ: `SaveQueue`). Tránh shared mutable state; nếu bắt buộc thì dùng `sync.RWMutex` hoặc `sync/atomic`.

**Q: Tôi cần map tạm thời trong hot loop, dùng gì?**  
→ Tạo map một lần ở phạm vi ngoài loop, dùng `clear(myMap)` để reset thay vì `myMap = make(map[K]V)`. `clear()` giữ nguyên allocated capacity.

**Q: Khi nào dùng `sync.Pool` vs khai báo biến local?**  
→ `sync.Pool` khi: slice/buffer được tạo và hủy lặp đi lặp lại ở tần suất cao (≥ game loop tick). Biến local khi: chỉ dùng trong một lần gọi hàm không hot.

**Q: Component của tôi chứa `[]Entity` (slice), tôi có cần Clone không?**  
→ Có. Bất kỳ Component nào chứa slice/map đều cần `Clone()` để tránh shared backing array dẫn đến data race khi concurrent read/write.

**Q: Tôi có thể dùng `fmt.Sprintf` trong hot loop không?**  
→ Không, trong hot loop. `fmt.Sprintf` luôn allocate. Dùng pre-built string hoặc byte builder nếu cần build string response.

---

## Cấu trúc package — Import rules

```
server/
├── peakgo/          ← Không import bất kỳ package nào trong server/ (chỉ stdlib)
│   ├── pool/
│   ├── codec/
│   ├── gmath/       ← chỉ import stdlib (math — none actually needed)
│   ├── rng/         ← chỉ import stdlib (math/rand, sync, sync/atomic, time)
│   ├── netio/       ← chỉ import peakgo/pool, peakgo/codec
│   └── loggate/     ← chỉ import server/logger
├── ecs/             ← Không import systems, game, server
├── models/          ← Không import systems, game, server
├── world/           ← Không import game, server
├── game/            ← Import ecs, models, world, protocol, peakgo/*
├── systems/         ← Import ecs, game, world, peakgo/*
└── server.go        ← Điều phối tất cả; import tất cả
```

> **Quy tắc tuyệt đối**: Không tạo import cycle. Package nằm ở tầng thấp hơn không được import tầng cao hơn.

---

## Benchmark tham chiếu (baseline đã đo)

| Benchmark | Baseline | Sau Peak Go |
|---|---|---|
| ReadHeader (length decode) | 32.89 ns/op, 2 B/op | 10.47 ns/op, **0 B/op** |
| ReadPayload (1 packet) | 148.1 ns/op, 512 B/op | 31.65 ns/op, **0 B/op** |
| ECS ComponentStore Set | ~3.2× chậm hơn | **0 B/op, 0 allocs/op** |
| Spatial Grid QueryRadius | heap alloc mỗi lần | **0 allocs/op** (pooled) |
| `gmath.DistanceSq` | N/A | **0.31 ns/op, 0 allocs** |
| `gmath.InBounds` | N/A | **0.32 ns/op, 0 allocs** |
| `rng.Float64()` (pooled) | `rand.New(...)` per call | **22 ns/op, 0 allocs** |
| `rng.Intn` (concurrent) | global mutex contention | **4.9 ns/op, 0 allocs** |

Để chạy lại toàn bộ benchmark baseline:
```bash
go test -bench=. -benchmem ./server/peakgo/...
go test -bench=. -benchmem ./server/ecs/...
go test -bench=. -benchmem ./server/protocol/...
```

---

*Handbook version: 1.0 — Được tạo cùng trạng thái Peak Go đầu tiên của dự án.*
