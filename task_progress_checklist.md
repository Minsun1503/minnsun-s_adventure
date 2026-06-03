# Benchmarks Optimization Plan

## Packages to fix (non-zero B/op or allocs/op):

### 1. eventbus (7-8 B/op)
- **Root cause**: `Event any` interface boxing when passing `int` to `Publish`
- **Fix**: Make `Bus` generic with `Bus[T any]` so channel sends avoid boxing

### 2. loggate (8 B/op DebugfDisabled, 72 B/op 2 allocs/op Infof)
- **Root cause**: Variadic `...any` creates caller-site slice allocation even when disabled
- **Fix**: Fast-path guard + lazy evaluation optimization

### 3. netio (2 B/op 1 alloc ReadHeader, 128 B/op 2 alloc WritePacket)
- **Root cause**: Need further investigation
- **Fix**: Will identify after examining full source

## Plan:
- [ ] Examine full source of eventbus.go, loggate.go, packet.go
- [ ] Fix eventbus: Make Bus generic to eliminate interface boxing
- [ ] Fix loggate: Optimize variadic guard patterns  
- [ ] Fix netio: Eliminate allocations in packet IO
- [ ] Run benchmarks to verify 0 B/op, 0 allocs/op for all three packages