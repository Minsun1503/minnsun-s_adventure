# Project Source of Truth

## 1. Những gì đã làm (What was done)
- Thêm hàm `send_notice_to_player(message string, conn net.Conn)` vào cuối file [server.go](file:///c:/Minnsun's Adventure/server/server.go) để trừu tượng hóa việc gửi gói tin/thông báo text đến client.
- Thay thế dòng ghi trực tiếp `conn.Write` chào mừng người chơi ở dòng 43 bằng lời gọi hàm `send_notice_to_player`.
- Cải tiến hàm [writeConn](file:///c:/Minnsun's Adventure/server/systems/systems.go#L51) trong [systems.go](file:///c:/Minnsun's Adventure/server/systems/systems.go) để thiết lập write deadline là 5 giây (tránh slow client block luồng) và tự động gọi `c.Close()` khi xảy ra lỗi ghi để buộc giải phóng và kích hoạt khối defer dọn dẹp trong [handleClient](file:///c:/Minnsun's Adventure/server/server.go#L76).
- Tích hợp thông báo tấn công đặc biệt cho quái vật (Special monster combat & death notices) vào [combat.go](file:///c:/Minnsun's Adventure/server/systems/combat.go) để định dạng và phát các tin thông báo dạng custom (`💀 [DEATH]`, `⚔️ [COMBAT]`) đồng thời tránh lỗi xung đột ghi đè dữ liệu (Copy-Modify-Overwrite) trong máy trạng thái AI.
- Thêm [InventoryComponent](file:///c:/Minnsun's Adventure/server/ecs/inventory_component.go) vào ECS Registry để quản lý đồ vật của người chơi và quái vật.
- Thêm cơ chế rơi đồ và bảng loot quái vật (Loot Drop & Monster Loot Tables) tại [loot.go](file:///c:/Minnsun's Adventure/server/systems/loot.go) trong package `systems` để loại bỏ vòng lặp phụ thuộc (import cycle) với package `models`.
- Tích hợp tính năng tự động rơi vật phẩm và cộng vào túi đồ của người chơi khi tiêu diệt quái vật thành công trực tiếp vào [AttackSystem](file:///c:/Minnsun's Adventure/server/systems/combat.go#L34).
- Thêm cơ chế tra cứu hòm đồ [RunInventoryQuerySystem](file:///c:/Minnsun's Adventure/server/systems/inventory_query.go) hỗ trợ lệnh `I` / `INV` của người chơi.
- Di chuyển bộ đăng ký vật phẩm [ItemRegistry](file:///c:/Minnsun's Adventure/server/systems/item.go) từ package `models` sang package `systems` để tránh vòng lặp phụ thuộc (import cycle) và khởi tạo chúng lúc khởi động Server.
- Nâng cấp [StatsComponent](file:///c:/Minnsun's Adventure/server/ecs/ecs.go#L30) để hỗ trợ giới hạn sinh mệnh tối đa `MaxHP` của người chơi và quái vật, đồng thời cập nhật logic khởi tạo cho chúng tại [player.go](file:///c:/Minnsun's Adventure/server/models/player.go) và [monster.go](file:///c:/Minnsun's Adventure/server/models/monster.go).
- Thêm hệ thống sử dụng vật phẩm [HandleItemUsageSystem](file:///c:/Minnsun's Adventure/server/systems/item_usage.go) để xử lý lệnh `USE [item_id]`, khấu trừ số lượng vật phẩm trong balo và hồi phục sinh mệnh an toàn dưới dạng giao thức ECS.
- Nâng cấp cấu trúc [PositionComponent](file:///c:/Minnsun's Adventure/server/ecs/ecs.go#L16) với trường `MapID` để hỗ trợ hệ thống phân vùng bản đồ.
- Tích hợp bộ phát tin phân vùng bản đồ [BroadcastToMap](file:///c:/Minnsun's Adventure/server/systems/broadcast.go) giúp giới hạn các thông điệp di chuyển và chiến đấu chỉ truyền tới những người chơi đứng cùng khu vực bản đồ.
- Tích hợp `MapID` vào cấu trúc phân vùng không gian [ChunkKey](file:///c:/Minnsun's Adventure/server/systems/spatial.go#L18) và các hàm chuyển đổi [worldToChunk](file:///c:/Minnsun's Adventure/server/systems/spatial.go#L57), truy vấn lân cận [QueryRadius](file:///c:/Minnsun's Adventure/server/systems/spatial.go#L154) trong [spatial.go](file:///c:/Minnsun's Adventure/server/systems/spatial.go) để ngăn chặn hoàn toàn việc kiểm tra chéo không gian giữa các Map khác nhau (khắc phục hoàn toàn lỗi quái vật đuổi bắt chéo bản đồ).
- Thêm hệ thống cổng dịch chuyển [ExecuteMapTransfer](file:///c:/Minnsun's Adventure/server/systems/gateway.go) hỗ trợ di chuyển an toàn giữa các map và đồng bộ vị trí thực thể tức thời vào Spatial Grid.
- Tích hợp lệnh điều phối dịch chuyển `WARP [Map_ID] [X] [Z]` vào cấu trúc điều hướng lệnh [handleCommand](file:///c:/Minnsun's Adventure/server/server.go#L117) của [server.go](file:///c:/Minnsun's Adventure/server/server.go).
- Loại bỏ toàn bộ emoji khỏi các gói tin mạng, thông báo lỗi và nhật ký thông báo hệ thống (portal, warp, item usage, command syntax) để chuẩn hóa giao tiếp text thuần túy.
- **Nâng cấp Giao thức Nhị phân (Binary Packet Protocol)**:
  - Thay đổi vòng lặp đọc của client trong `handleClient` ở [server.go](file:///c:/Minnsun's Adventure/server/server.go) sang giao thức nhị phân dạng `[Length uint16] [Opcode uint8] [Payload N-bytes]`.
  - Thay thế hàm `handleCommand` thành `handleBinaryPacket` điều phối gói tin theo `Opcode` (1: MOVE, 2: INV, 3: USE, 4: WARP, 5: ATTACK, 6: INFO, 7: QUIT).
  - Nâng cấp [HandlePlayerMovementSystem](file:///c:/Minnsun's Adventure/server/systems/movement.go) giải mã tọa độ X, Z (int32 BE) từ payload nhị phân.
  - Nâng cấp [HandleItemUsageSystem](file:///c:/Minnsun's Adventure/server/systems/item_usage.go) giải mã `ItemID` (uint64 BE) từ payload nhị phân.
  - Thêm `HandleWarpSystem` trong [systems/gateway.go](file:///c:/Minnsun's Adventure/server/systems/gateway.go) giải mã `MapID`, `X`, Z (int32 BE) từ payload nhị phân.
  - Tạo file mock client nhị phân tại [mock_client.go](file:///C:/Users/84845.DESKTOP-6UDUU1B/.gemini/antigravity-ide/brain/1b3e3809-efc1-4008-b4b9-eca00115a78d/scratch/mock_client.go) để kiểm thử giao thức mới.
- **Hệ thống Vật phẩm trên Mặt đất & Phân rã theo thời gian (Ground Item Drops & Lifetime Despawn)**:
  - Thêm [LifetimeComponent](file:///c:/Minnsun's Adventure/server/ecs/lifetime_component.go) để theo dõi thời gian bắt đầu và thời lượng tồn tại của vật phẩm rơi trên đất.
  - Cập nhật [ecs.go](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) để đăng ký cột dữ liệu `lifetimes` và thực hiện dọn dẹp song song khi giải phóng thực thể trong `RemoveEntity` (nâng wg.Add từ 6 lên 7 để dọn sạch sẽ tránh rò rỉ).
  - Viết hệ thống sinh vật phẩm `SpawnItemOnGround` và hệ thống quét phân rã `RunGroundItemDecaySystem` tại [systems/ground_item.go](file:///c:/Minnsun's Adventure/server/systems/ground_item.go) tự động dọn dẹp các vật phẩm hết hạn và đồng bộ ra khỏi `GlobalSpatialGrid` tránh lỗi ma thực thể (tích hợp **Spawn Guard** từ chối nạp template lỗi để tránh dữ liệu ảo).
  - Tích hợp gọi hệ thống phân rã vào heartbeat game loop tại [gameloop.go](file:///c:/Minnsun's Adventure/server/systems/gameloop.go) chạy nền 250ms định kỳ.
  - Tích hợp cơ chế sinh vật phẩm rơi tự do trên mặt đất khi quái vật bị tiêu diệt vào [combat.go](file:///c:/Minnsun's Adventure/server/systems/combat.go#L92), thay thế hoàn toàn việc tự động cộng đồ thẳng vào balo người chơi. Đồng thời áp dụng **Loot Scattering** tạo độ lệch ngẫu nhiên nhỏ (±1 ô tọa độ) và clamp trong biên giới bản đồ (0-100) để phân tán vật phẩm rơi trực quan hơn.
- **Hệ thống Nhặt vật phẩm dưới đất (Ground Loot Pickup System)**:
  - Thiết lập hệ thống [pickup.go](file:///c:/Minnsun's Adventure/server/systems/pickup.go) xử lý hàm `HandleItemPickupSystem` kiểm tra khoảng cách an toàn (dưới 5.0 đơn vị), xóa thực thể khỏi Spatial Grid và Registry tức thời để ngăn chặn lỗi trùng lặp (dupe) đồ, và chuyển vật phẩm vào túi đồ của người chơi.
  - Đăng ký **Opcode 8 (PICKUP)** trong [server.go](file:///c:/Minnsun's Adventure/server/server.go) để nhận diện yêu cầu và giải mã ID vật phẩm rơi (uint64 BE, 8 bytes) chuyển tiếp qua Pickup System.
  - Cập nhật client giả lập tại [mock_client.go](file:///C:/Users/84845.DESKTOP-6UDUU1B/.gemini/antigravity-ide/brain/1b3e3809-efc1-4008-b4b9-eca00115a78d/scratch/mock_client.go) để kiểm thử gói tin nhặt đồ nhị phân (Opcode 8) gửi về máy chủ.
- **Trang bị slots & Tính toán chỉ số linh hoạt (Equipment slots & Stat Aggregator)**:
  - Thêm [EquipmentComponent](file:///c:/Minnsun's Adventure/server/ecs/equipment_component.go) để theo dõi trạng thái trang bị của thực thể (Weapon, Armor).
  - Cập nhật [ecs.go](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) để đăng ký cột `equipment`, nâng số lượng luồng dọn dẹp song song trong `RemoveEntity` lên **9 luồng** (`wg.Add(9)`) tránh rò rỉ khi người chơi thoát, và cung cấp các hàm helper Get/Set.
  - Nâng cấp [ItemTemplate](file:///c:/Minnsun's Adventure/server/systems/item.go#L4) để hỗ trợ các chỉ số cộng thêm (`SlotType`, `BonusDam`, `BonusHP`) và khởi tạo sẵn trang bị mẫu (`Iron Sword` cộng 15 Dam, `Leather Armor` cộng 30 HP).
  - Xây dựng hệ thống tái tính toán chỉ số [RecalculateActiveStats](file:///c:/Minnsun's Adventure/server/systems/stat_engine.go#L9) làm nguồn chân lý duy nhất (Single Source of Truth) kết hợp chỉ số cơ bản của người chơi với chỉ số cộng thêm từ trang bị.
  - Xây dựng hệ thống xử lý trang bị đồ [HandleEquipmentSystem](file:///c:/Minnsun's Adventure/server/systems/equipment.go#L9) để kiểm tra túi đồ, cập nhật khe cắm và tự động cập nhật lại combat stats.
  - Cập nhật [player.go](file:///c:/Minnsun's Adventure/server/models/player.go#L19) để khởi tạo rỗng các trang bị khi người chơi mới kết nối.
  - Đăng ký **Opcode 9 (EQUIP)** tại [server.go](file:///c:/Minnsun's Adventure/server/server.go) nhận diện yêu cầu trang bị nhị phân chứa ID vật phẩm (uint64 BE, 8 bytes).
  - Cập nhật client giả lập tại [mock_client.go](file:///C:/Users/84845.DESKTOP-6UDUU1B/.gemini/antigravity-ide/brain/1b3e3809-efc1-4008-b4b9-eca00115a78d/scratch/mock_client.go) để kiểm thử gói tin trang bị nhị phân (Opcode 9).
- **Hệ thống Lưới va chạm & Cản vật thể (Map Obstacles & Collision Blocking)**:
  - Thêm [map_collision.go](file:///c:/Minnsun's Adventure/server/systems/map_collision.go) định nghĩa lưới va chạm `MapCollisionGrid` kích thước 101x101 để lưu trữ mảng cản tĩnh O(1) tránh duyệt danh sách ngầm. Thiết lập cản Town Square (50, 50) và dải tường Z=15 (X: 10-20) cho bản đồ số 1.
  - Tích hợp hàm check va chạm `IsTileBlocked` trực tiếp vào hàm chuyển động [MovementSystem](file:///c:/Minnsun's Adventure/server/systems/systems.go#L76) của người chơi và quái vật. Mọi di chuyển vào ô cản sẽ bị từ chối chuyển động với cảnh báo text sạch emoji.
  - Cập nhật thuật toán sinh bước đi ngẫu nhiên [roamStep](file:///c:/Minnsun's Adventure/server/systems/ai_roaming.go#L267) của quái vật AI để bỏ qua các tọa độ bị cản, tránh việc AI đi xuyên tường/vật thể.
  - Kích hoạt khởi tạo lưới cản `InitializeCollisionMaps` trong main của [server.go](file:///c:/Minnsun's Adventure/server/server.go#L16) lúc Server boot.
- **Hệ thống Tìm đường thông minh của quái vật AI (AI Localized Pathfinding)**:
  - Thiết lập [pathfinding.go](file:///c:/Minnsun's Adventure/server/systems/pathfinding.go) cài đặt hàm `FindPath` thuật toán BFS để quái vật tự động tìm hướng đi vòng tránh ô cản trên lưới 2D.
  - Tích hợp giới hạn tìm kiếm tối đa 400 nút duyệt để ngăn chặn suy giảm hiệu năng Game Loop (heartbeat 250ms).
  - Cập nhật [ai_roaming.go](file:///c:/Minnsun's Adventure/server/systems/ai_roaming.go) áp dụng `FindPath` thay thế cho hàm đi thẳng `stepToward` khi quái vật đang đuổi bắt người chơi (`Chasing`) hoặc quay trở lại spawn (`Returning`).
  - **Tối ưu hóa Memory Pool**: Triển khai `sync.Pool` tái sử dụng bộ đệm queue slice và maps thông qua struct `pathfindContext`, sử dụng hàm `clear()` tích hợp của Go để dọn dẹp bộ đệm map mà không giải phóng dung lượng đã cấp phát, triệt tiêu hoàn toàn heap allocations của thuật toán tìm đường trên Game Loop.
- Tái cấu trúc toàn bộ mã nguồn sang kiến trúc **ECS (Entity-Component-System)** hoàn chỉnh:
  - Tạo package [ecs](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) chứa thực thể `Entity` dạng `uint64` (thay cho dạng string) giúp tối ưu hóa tra cứu O(1) tránh băm chuỗi, và lưu trữ Component dưới dạng Inline value tránh cấp phát bộ nhớ phụ (heap allocs).
  - Tối ưu hóa Registry sử dụng cấu trúc `TypedSyncMap` an toàn luồng bọc quanh `sync.Map` được cài đặt tại [state.go](file:///c:/Minnsun's Adventure/server/state/state.go) giúp loại bỏ hoàn toàn cơ chế Mutex thô, đồng thời thực hiện xóa các Component song song thông qua `sync.WaitGroup`.
  - Loại bỏ hoàn toàn package `network` (đã xóa thư mục `network`) do không còn sử dụng.
  - Tạo package [systems](file:///c:/Minnsun's Adventure/server/systems/systems.go) xử lý toàn bộ logic nghiệp vụ tác động lên thực thể thông qua component (`BroadcastSystem`, `SendNoticeSystem`, `MovementSystem` nhận đầu vào tọa độ kiểu `int` và thực hiện xác thực ranh giới bản đồ 100x100 làm rào chắn bảo mật, `GetInfoSystem`).
  - Chuyển đổi cấu trúc và logic khởi tạo thực thể, nạp component về [player.go](file:///c:/Minnsun's Adventure/server/models/player.go) và [monster.go](file:///c:/Minnsun's Adventure/server/models/monster.go) (đổi tên quái vật thô thành `MonsterTemplate`, đồng thời thêm hàm tiện ích `SpawnMonsterFromTemplate` có đầy đủ chú thích tiếng Anh chuẩn Go doc để sinh thực thể quái vật sống trực tiếp trên bản đồ thông qua ECS).
  - Cập nhật [server.go](file:///c:/Minnsun's Adventure/server/server.go) và [broadcast.go](file:///c:/Minnsun's Adventure/server/network/broadcast.go) để kết nối đồng bộ thông qua Registry của ECS, parse tọa độ sang `int` trước khi thực hiện chuyển qua các System tương ứng.
  - Thiết lập và khởi chạy **Game Loop (Heartbeat)** chu kỳ 250ms chạy nền dưới dạng goroutine được đóng gói sạch sẽ trong file [gameloop.go](file:///c:/Minnsun's Adventure/server/systems/gameloop.go) thuộc package `systems`, tích hợp kiểm tra tối ưu hóa tắt/bật tính toán AI dựa trên việc quét tìm thực thể người chơi thực tế thông qua các Component của ECS.
  - Thiết lập file `.gitignore` chung để hỗ trợ cả dự án Go (bỏ file thực thi `.exe`, `.msi`, v.v.) và dự án Unity Client tại `client/Minnsun's Adventure` (bỏ các thư mục tạm thời `Library/`, `Temp/`, `Obj/`, `Logs/`, `UserSettings/`, các file IDE tự sinh như `.csproj`, `.sln`, `.slnx` mà vẫn bảo tồn đầy đủ các assets và file `.meta`).
  - Thực hiện dọn dẹp bộ nhớ đệm index của git (`git rm -r --cached`) và đưa các assets Unity hợp lệ vào khu vực staging.
  - Tích hợp `AIComponent` vào ECS Registry: Thêm trường `ai` kiểu `state.TypedSyncMap[Entity, AIComponent]` vào struct `Registry` trong [ecs.go](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) và thực hiện xóa dọn dẹp AIComponent khi giải phóng Entity trong `RemoveEntity`.
  - Khắc phục lỗi biên dịch `MonsterTemplate` bằng cách tách biệt rõ ràng việc đăng ký static template (`CreateMonsterEntity` sử dụng ID tĩnh) và sinh thực thể quái vật sống (`SpawnMonsterFromTemplate` khởi tạo tọa độ thực tế, đăng ký vào spatial grid và kích hoạt AIComponent).
  - Tự động sinh (spawn) 5 thực thể quái vật sống (Slime, Wild Boar, Sea King Proto) từ template tại các tọa độ khởi tạo khác nhau khi Server khởi động thành công.
- **Tích hợp hệ thống lưu trữ dữ liệu MySQL không chặn (Asynchronous Database Save Queue via MySQL)**:
  - Khởi tạo Database MySQL và tạo các bảng `characters` (chứa dữ liệu tĩnh như tên người chơi), `character_states` (chứa dữ liệu động như vị trí, sinh mệnh, sát thương, trang bị) và `character_inventory` (balo hòm đồ) khi Server boot thông qua DSN `root:root@tcp(127.0.0.1:3306)/?parseTime=true`.
  - Triển khai **Hàng đợi Kết nối & Thao tác Thao tác Đăng nhập (Throttled Login Queue / Connection Pool)** thông qua channel đệm `LoginQueue` (kích thước 1000) và 4 luồng chạy nền `StartLoginWorkerPool`. Khi client kết nối, socket được đẩy non-blocking vào hàng đợi, tránh block hoàn toàn luồng listener chấp nhận kết nối chính (`Accept()`).
  - Thực hiện lưu thông tin tĩnh của người chơi (ID & Name) vào bảng `characters` một lần duy nhất khi họ kết nối thành công tại `CreatePlayerEntity` trong [models/player.go](file:///c:/Minnsun's Adventure/server/models/player.go). Nếu quá trình ghi DB tĩnh thất bại, Server từ chối kết nối (Handshake Connection Guard) để đảm bảo không rò rỉ hoặc sai lệch khóa ngoại.
  - Khởi chạy luồng chạy nền Save Worker Engine (`StartSaveWorkerEngine`) lắng nghe channel `SaveQueue` để thực hiện ghi cơ sở dữ liệu bất đồng bộ. Hàng đợi này chỉ thực hiện cập nhật dữ liệu động (`character_states`, `character_inventory`) khi người chơi ngắt kết nối (`QUIT`), tối ưu hóa I/O bằng cách loại bỏ hoàn toàn các câu lệnh ghi dữ liệu tĩnh lặp đi lặp lại.
  - Tích hợp `systems.QueuePlayerSave(playerEntity)` vào defer block dọn dẹp kết nối trong `handleClient` của [server.go](file:///c:/Minnsun's Adventure/server/server.go) để lưu trạng thái người chơi trước khi RemoveEntity dọn dẹp các component trong RAM.
  - Giải quyết lỗi chu kỳ vòng lặp import (Import Cycle Error): Loại bỏ import `"server/systems"` và lời gọi update spatial grid ra khỏi [models/monster.go](file:///c:/Minnsun's Adventure/server/models/monster.go). Di chuyển cơ chế đăng ký Grid của quái vật khi Spawn lên hàm `main` của [server.go](file:///c:/Minnsun's Adventure/server/server.go), đảm bảo tính tree-structured của các dependency package trong Go.
  - Sử dụng cú pháp MySQL `ON DUPLICATE KEY UPDATE` cho các câu lệnh Upsert để đảm bảo tương thích tốt nhất với hệ quản trị cơ sở dữ liệu MySQL (thay vì SQLite `ON CONFLICT` nguyên bản).
  - Tạo file SQL schema tham chiếu chi tiết tại [schema.sql](file:///c:/Minnsun's Adventure/server/schema.sql) để dễ dàng kiểm tra cấu trúc bảng.
  - Tích hợp ràng buộc duy nhất `UNIQUE KEY idx_char_name (name)` cho bảng `characters` nhằm tối ưu hóa tra cứu và tránh trùng lặp tên nhân vật.
  - Áp dụng `context.WithTimeout` cho mọi thao tác DB: 3 giây cho login (`CreatePlayerEntity` trong [player.go](file:///c:/Minnsun's Adventure/server/models/player.go)), 5 giây cho save worker (`executeWriteToSQL` trong [save_engine.go](file:///c:/Minnsun's Adventure/server/systems/save_engine.go)). Giải phóng luồng worker nhanh chóng khi DB gặp sự cố.
- **Hệ thống Gói Tin Lỗi Nhị phân (Binary Error Opcode)**:
  - Tạo file [error_packet.go](file:///c:/Minnsun's Adventure/server/systems/error_packet.go) định nghĩa Opcode `0xFF` (255) dành riêng cho phản hồi lỗi hệ thống từ Server về Client.
  - Format gói tin: `[Length uint16 BE][Opcode 0xFF][ErrorCode uint16 BE][MessageLen uint16 BE][Message UTF-8]`.
  - Các mã lỗi được định nghĩa sẵn: `ErrCodeServerFull (1)` — hàng đợi Login đầy, `ErrCodeDatabaseError (2)` — lỗi DB timeout/failure, `ErrCodeInternalError (3)` — lỗi nội bộ không mong đợi.
  - Hàm `SendErrorPacket(conn, errorCode, message)` ghi trực tiếp vào `net.Conn` thô (không cần entity ECS) vì tại thời điểm lỗi login, entity chưa tồn tại trong Registry.
  - Thay thế tất cả các lời gọi `conn.Write([]byte("Error: ..."))` text thô trong [server.go](file:///c:/Minnsun's Adventure/server/server.go) bằng `SendErrorPacket` nhị phân, giúp Unity Client parse `ErrorCode` để hiển thị popup lỗi chính xác thay vì chuỗi text thô.
- **Tích hợp tập tin quy chuẩn Cline (.clinerules & Hướng dẫn ECS)**:
  - Khởi tạo tập tin [`.clinerules`](file:///c:/Minnsun's Adventure/.clinerules) ở thư mục gốc chứa các quy tắc thiết kế ECS cao cấp, tối ưu hóa bộ nhớ, và cấu trúc gói tin nhị phân.
  - Viết tài liệu [`ecs_guide_cline.md`](file:///c:/Minnsun's Adventure/knowledge/ecs_guide_cline.md) cung cấp hướng dẫn nghiệp vụ và luồng phát triển chuẩn hóa hệ thống cho các AI coding assistants.
- **Tái cấu trúc và Phân nhóm Systems Phase 2 (17 files còn lại)**:
  - Tạo 2 package mới: [`server/game`](file:///c:/Minnsun's Adventure/server/game) (Gameplay logic) và [`server/db`](file:///c:/Minnsun's Adventure/server/db) (Database persistence).
  - Di chuyển `broadcast.go` sang [`server/protocol`](file:///c:/Minnsun's Adventure/server/protocol) và đổi thành `package protocol`.
  - Di chuyển `gateway.go` và `proximity.go` sang [`server/world`](file:///c:/Minnsun's Adventure/server/world) và đổi thành `package world`.
  - Di chuyển `save_engine.go` sang [`server/db`](file:///c:/Minnsun's Adventure/server/db) và đổi thành `package db`.
  - Di chuyển 11 file gameplay còn lại (`ai_roaming.go`, `combat.go`, `loot.go`, `item.go`, `item_usage.go`, `inventory_query.go`, `equipment.go`, `stat_engine.go`, `ground_item.go`, `pickup.go`, `movement.go`) sang [`server/game`](file:///c:/Minnsun's Adventure/server/game) và đổi thành `package game`.
  - Giữ lại `systems.go` và `gameloop.go` ở package `systems` làm bộ điều phối trung tâm.
  - Hoàn tất cập nhật toàn bộ import chéo giữa các package và build/vet kiểm thử thành công.
- **Khôi phục trạng thái nhân vật khi đăng nhập (MySQL Player Load)**:
  - Thêm hàm `loadSavedPlayerState(name)` trong [player.go](file:///c:/Minnsun's Adventure/server/models/player.go) để truy vấn MySQL lấy vị trí (X, Z, MapID), chỉ số (HP, MaxHP, Dam), trang bị (Weapon, Armor) và balo (Inventory) của người chơi cũ bằng `name`.
  - Thay đổi logic `CreatePlayerEntity` để tự động khôi phục dữ liệu đã lưu vào Registry ECS thay vì gán các chỉ số mặc định của nhân vật mới tinh.
  - Sử dụng cơ chế Transaction (`tx`) và xóa bản ghi cũ trước khi chèn ID thực thể mới (`DELETE` -> `INSERT`) nhằm đồng bộ ID thực thể dạng động trong RAM với MySQL.
- **Hệ thống Đăng ký & Đăng nhập bảo mật có Mật khẩu (Password Authentication & Registration System)**:
  - Tích hợp thư viện bảo mật `golang.org/x/crypto/bcrypt` để mã hóa mật khẩu một cách an toàn (cost 10).
  - Khai báo opcode C2S mới `OpcodeC2SLogin = 10` và `OpcodeC2SRegister = 11` tại [`opcodes.go`](file:///c:/Minnsun's Adventure/server/protocol/opcodes.go). Cả hai gói tin cùng cấu trúc payload nhị phân: `[UserLen uint8][Username string][PassLen uint8][Password string]`.
  - Cập nhật schema bổ sung trường `password_hash VARCHAR(255)` tại [`schema.sql`](file:///c:/Minnsun's Adventure/server/schema.sql) và [`db.go`](file:///c:/Minnsun's Adventure/server/models/db.go) (đã tích hợp câu lệnh tự động nâng cấp `ALTER TABLE` đối với dữ liệu hiện hữu).
  - Xây dựng hệ thống `RegisterNewAccount` tại [`player.go`](file:///c:/Minnsun's Adventure/server/models/player.go) để kiểm thử trùng lặp, băm mật khẩu bằng `bcrypt` và lưu vào DB.
  - Cập nhật `processLogin(conn)` ở [`server.go`](file:///c:/Minnsun's Adventure/server/server.go) để phân luồng điều hướng gói tin LOGIN hoặc REGISTER gửi lên đầu tiên dưới sự bảo vệ của Read Deadline.
  - Khắc phục bug nghiêm trọng: Đảm bảo bảo tồn trường `password_hash` khi giải phóng và chèn lại ID thực thể động lúc người chơi đăng nhập lại game.
- **Hệ thống tự động hồi sinh quái vật (Automated Monster Respawn Scheduler)**:
  - Tạo tệp [`respawn.go`](file:///c:/Minnsun's Adventure/server/game/respawn.go) quản lý hàng đợi hồi sinh quái vật `GlobalRespawnManager` một cách an toàn luồng (Mutex).
  - Tích hợp hàm `RunRespawnSystem()` vào nhịp game loop heartbeat 250ms định kỳ tại [`gameloop.go`](file:///c:/Minnsun's Adventure/server/systems/gameloop.go). Khi thời gian chờ kết thúc, hệ thống gọi `models.SpawnMonsterFromTemplate` để tái tạo thực thể quái vật và tự động cập nhật vị trí lên Spatial Grid.
  - Bổ sung hàm `GetTemplateByName` tại [`monster.go`](file:///c:/Minnsun's Adventure/server/models/monster.go) để tra cứu ID mẫu quái vật dựa trên Metadata Name.
  - Cấu hình DeathSystem trong [`combat.go`](file:///c:/Minnsun's Adventure/server/game/combat.go) để tự động đăng ký sự kiện hồi sinh trễ 15 giây khi quái vật bị tiêu diệt.
- **Hệ thống Tổ đội ECS và Kênh Sự kiện Chia sẻ (ECS Player Party System and Shared Event Channels)**:
  - Khai báo cấu trúc `PartyComponent` (dành cho thực thể tổ đội ảo) và `PartyMemberComponent` (đính kèm vào thực thể người chơi) tại [`ecs.go`](file:///c:/Minnsun's Adventure/server/ecs/ecs.go).
  - Cập nhật hàm giải phóng thực thể `RemoveEntity` trong Registry để dọn dẹp các component tổ đội song song nhằm tránh rò rỉ bộ nhớ (tăng WaitGroup lên **11 luồng**).
  - Xây dựng các hàm xử lý logic tổ đội tại [`party.go`](file:///c:/Minnsun's Adventure/server/game/party.go) bao gồm `CreatePartySystem` để khởi tạo tổ đội và gán đội trưởng, `BroadcastToParty` để phát gói tin text tới tất cả thành viên trực thuộc, và `GetPlayerPartyID` để lấy Party ID của người chơi.
  - Đăng ký opcode C2S mới `OpcodeC2SPartyCreate = 12` với payload dạng `[TeamNameLen uint8][TeamName string]` tại [`opcodes.go`](file:///c:/Minnsun's Adventure/server/protocol/opcodes.go).
  - Tích hợp trường hợp xử lý gói tin Opcode 12 nhị phân vào hàm `handleBinaryPacket` trong [`server.go`](file:///c:/Minnsun's Adventure/server/server.go) để cho phép người chơi tạo tổ đội.
  - Cập nhật logic `DeathSystem` tại [`combat.go`](file:///c:/Minnsun's Adventure/server/game/combat.go) để kiểm tra nếu kẻ tiêu diệt quái vật thuộc một tổ đội thì phát thông báo chiến thắng (`[COMBAT]`) đến toàn bộ thành viên tổ đội thay vì phát rộng rãi ra toàn bản đồ.
- **Hệ thống Giao dịch Trực tiếp Bảo mật hai pha (Peer-to-Peer Secure Trading System)**:
  - Thiết lập cấu trúc `TradeOffer` và `TradeSession` điều hành phiên giao dịch độc lập tại [`trade.go`](file:///c:/Minnsun's Adventure/server/game/trade.go).
  - Áp dụng kỹ thuật khóa hai pha: Phase 1 xác nhận khóa trạng thái giao dịch (`LockTradeStage`) và Phase 2 hoán đổi vật phẩm nguyên tử (`ExecuteAtomicTradeSwap`) qua Copy-Modify-Overwrite an toàn luồng, loại bỏ nguy cơ trùng lặp (dupe) đồ.
  - Đăng ký bốn opcode C2S giao dịch mới tại [`opcodes.go`](file:///c:/Minnsun's Adventure/server/protocol/opcodes.go): `OpcodeC2STradeInit (15)`, `OpcodeC2STradeOffer (16)`, `OpcodeC2STradeConfirm (17)`, và `OpcodeC2STradeCancel (18)`.
  - Định tuyến các gói tin nhị phân giao dịch trực tiếp trong `handleBinaryPacket` tại [`server.go`](file:///c:/Minnsun's Adventure/server/server.go).
  - Tích hợp tự động hủy phiên giao dịch an toàn khi người chơi ngắt kết nối đột ngột (`CancelTradeSession`) trong defer block của `handleClient`.
- **Hệ thống Điểm kinh nghiệm (XP) và Thăng cấp Nhân vật (Character XP and Level-Up Progression System)**:
  - Nâng cấp cấu trúc `StatsComponent` tại [`ecs.go`](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) để theo dõi cấp độ (`Level`) và điểm kinh nghiệm (`XP`).
  - Thiết lập công thức tính toán ngưỡng thăng cấp lũy thừa bậc $1.8$: $\text{Required XP} = 100 \times L^{1.8}$ và các logic cộng chỉ số thuộc tính cơ bản (+25 MaxHP, +3 Dam) khi thăng cấp tại tệp mới [`progression.go`](file:///c:/Minnsun's Adventure/server/game/progression.go).
  - Tích hợp lưu trữ và tự động phục hồi chỉ số cấp độ/kinh nghiệm khi khởi động lại Server vào bảng `character_states` của MySQL tại [`player.go`](file:///c:/Minnsun's Adventure/server/models/player.go), [`db.go`](file:///c:/Minnsun's Adventure/server/models/db.go), và [`save_engine.go`](file:///c:/Minnsun's Adventure/server/db/save_engine.go).
  - Nâng cấp `MonsterTemplate` để hỗ trợ lưu trữ chỉ số điểm thưởng kinh nghiệm (`XPReward`) trong tệp cấu hình JSON `data/monster_templates.json`.
  - Phân bổ tự động điểm thưởng kinh nghiệm sau khi tiêu diệt quái vật thành công trực tiếp trong `DeathSystem` tại [`combat.go`](file:///c:/Minnsun's Adventure/server/game/combat.go), hỗ trợ chia đều điểm kinh nghiệm cho các thành viên tổ đội online nếu người chơi thuộc một tổ đội.
- **Hệ thống Kỹ năng/Phép thuật và Cổng xác thực tài nguyên (Skill/Spell Casting System and Resource Validation Gates)**:
  - Mở rộng cấu trúc `StatsComponent` trong [`ecs.go`](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) để hỗ trợ chỉ số năng lượng `MP` và `MaxMP`.
  - Thiết lập thực thể mẫu kỹ năng `SkillTemplate` cùng cơ chế đăng ký và khởi tạo chiêu thức (Fireball tiêu tốn 20 MP gây 2.5x sát thương, Thunderclap tiêu tốn 35 MP gây 4x sát thương) tại tệp mới [`skills.go`](file:///c:/Minnsun's Adventure/server/models/skills.go).
  - Cập nhật cơ sở dữ liệu MySQL (bảng `character_states`) để tự động lưu trữ, nâng cấp cấp độ (`MaxMP += 10` mỗi cấp và hồi phục đầy `MP`) và phục hồi năng lượng phép thuật cho nhân vật.
  - Xây dựng hệ thống kích hoạt phép thuật [`skills.go`](file:///c:/Minnsun's Adventure/server/game/skills.go) thực hiện tuần tự 3 chặng kiểm tra tài nguyên (Mana Gate Check), khấu trừ năng lượng (Atomic Cost Deduction) và nhân sát thương theo tỷ lệ (Multiplier Damage Projection). Hỗ trợ kiểm soát tầm đánh phép thuật dưới 6.0 đơn vị không gian.
  - Đăng ký opcode C2S nhị phân mới `OpcodeC2SSkillCast = 19` với payload `[SkillID uint64 BE][TargetEntityID uint64 BE]` và xử lý tại [`server.go`](file:///c:/Minnsun's Adventure/server/server.go).
- **Hệ thống Kênh Trò chuyện Phân vùng & Toàn cầu (Global Chat Channel System & Map-Isolated Broadcast Channels)**:
  - Xây dựng bộ điều hướng hội thoại [`chat.go`](file:///c:/Minnsun's Adventure/server/game/chat.go) hỗ trợ phân luồng 3 kênh trò chuyện độc lập: Kênh Bản đồ (`[MAP]`, mặc định fallback), Kênh Tổ đội (`[PARTY]`, bắt đầu bằng `/p ` hoặc `/party `), và Kênh Toàn cầu (`[GLOBAL]`, bắt đầu bằng `/g ` hoặc `/global `).
  - Triển khai hàm `BroadcastToWorld` gửi gói tin trực tiếp tới tất cả thực thể đang hoạt động có đính kèm `ConnectionComponent`.
  - Đăng ký opcode C2S nhị phân mới `OpcodeC2SChat = 20` với payload chứa chuỗi UTF-8 tin nhắn và tích hợp bộ xử lý trong `handleBinaryPacket` tại [`server.go`](file:///c:/Minnsun's Adventure/server/server.go).
- **Hệ thống Hiệu ứng Trạng thái, Buff, và Tích tắc thời gian (Status Effects, Buffs, and Over-Time Tickers)**:
  - Định nghĩa cấu trúc `ActiveEffect` (Type, Value, Duration, LastTickTime) và `EffectsComponent` lưu trữ danh sách hiệu ứng tạm thời tại [`effects_component.go`](file:///c:/Minnsun's Adventure/server/ecs/effects_component.go).
  - Tích hợp `effects` component registry vào [`ecs.go`](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) cùng các hàm helper (`SetEffects`, `GetEffects`, `DeleteEffects`) và dọn dẹp song song trong `RemoveEntity` (nâng WaitGroup lên **12 luồng**).
  - Xây dựng luồng xử lý hiệu ứng [`effects_system.go`](file:///c:/Minnsun's Adventure/server/game/effects_system.go) trừ dần thời gian hiệu lực sau mỗi nhịp 250ms, tự động gọi `RecalculateActiveStats` khi các buff chỉ số (như `haste_buff`) hết hạn, và áp dụng sát thương rút máu định kỳ 1 giây (DoT downsampling) cho Poison/Burn, hỗ trợ xử lý tử vong của người chơi/quái vật do DoT gây ra.
  - Tích hợp gọi hệ thống hiệu ứng `game.RunStatusEffectsSystem()` trực tiếp vào game loop heartbeat tại [`gameloop.go`](file:///c:/Minnsun's Adventure/server/systems/gameloop.go).
- **Hệ thống Ghi Log Thống Nhất & Debug Toàn Diện (Unified Tracing & Logging Engine)**:
  - Tạo package [`server/logger/`](file:///c:/Minnsun's Adventure/server/logger/) chứa 2 file cốt lõi:
    - [`logger.go`](file:///c:/Minnsun's Adventure/server/logger/logger.go): Async channel-based logger (4096 slot buffer) với 4 cấp độ `DEBUG/INFO/WARN/ERROR`. Bật/tắt DebugMode qua [`server/data/config.json`](file:///c:/Minnsun's Adventure/server/data/config.json). Xuất log ra Console (màu ANSI) và File xoay vòng theo ngày + kích thước (10MB/file) dưới thư mục `logs/`. Worker goroutine đơn độc drain channel → tuyệt đối không block game loop.
    - [`entity_tracer.go`](file:///c:/Minnsun's Adventure/server/logger/entity_tracer.go): Watch-list thread-safe để theo dõi bất kỳ Entity ID nào. Gọi `GlobalEntityTracer.Watch(id)` để đăng ký, sau đó mọi system gọi `TraceEvent(id, "HP_CHANGE", ...)` sẽ in chi tiết ra console khi DebugMode = true.
  - Tích hợp **Network Packet Tracer** tại `handleBinaryPacket` trong [`server.go`](file:///c:/Minnsun's Adventure/server/server.go): Khi `debug=true`, mỗi gói tin nhị phân từ Client sẽ được in dạng Hex dump kèm tên Opcode (`[NET RX] Conn: ... | Opcode: 5 (ATTACK) | Hex: [...]`).
  - Tích hợp **Performance Profiler** tại 3 điểm nghẽn nhạy cảm nhất: Game Loop (cảnh báo nếu tick > 50ms), BFS Pathfinding (cảnh báo nếu > 5ms), DB Write (cảnh báo nếu > 100ms).
  - Thay thế hoàn toàn **30+ điểm** `fmt.Printf/Println` rải rác trong toàn source sang các lời gọi `logger.*` có phân cấp (`DEBUG` cho AI state, movement, combat detail; `INFO` cho connect/spawn/save; `WARN` cho queue full/retry; `ERROR` cho DB failure/boot error).
  - Audit bằng `grep "fmt.Print" ./...` xác nhận **Zero fmt.Print còn sót lại** trên toàn codebase.


  - Khắc phục lỗ hổng ép kiểu không an toàn (unsafe type assertion panic) trong hàm `Get` tại [`state.go`](file:///c:/Minnsun's Adventure/server/state/state.go) bằng comma-ok check.
  - Khắc phục lỗi TOCTOU race (drop entry khi chunk bị thu hồi) và nguy cơ deadlock của double RLock trong [`spatial.go`](file:///c:/Minnsun's Adventure/server/world/spatial.go).
  - Khắc phục rò rỉ bộ nhớ (backing array leak) của hàng đợi BFS và cấp phát rác trên Heap của các slice hướng di chuyển (`dirs`) tại [`pathfinding.go`](file:///c:/Minnsun's Adventure/server/world/pathfinding.go) thông qua cơ chế con trỏ head index và định nghĩa tĩnh phạm vi package-level.
  - Khắc phục nút thắt ghi đĩa tuần tự và cảnh báo mất mát dữ liệu tại [`save_engine.go`](file:///c:/Minnsun's Adventure/server/db/save_engine.go) bằng cách triển khai cơ chế lô gói tin (Batch Upsert Inventory), ghi log cảnh báo khi hàng đợi đầy, và lặp lại thao tác ghi đĩa tự động (Transient Error Retries) lên đến 3 lần.
  - **Nâng cấp Hệ Thống ECS & Phòng Chống Data Race**:
    - Triển khai cơ chế Copy-on-Write (CoW) bằng cách thêm phương thức `.Clone()` cho các linh kiện có kiểu dữ liệu tham chiếu (Reference Types) bao gồm: `InventoryComponent` (sao chép bản đồ items), `PartyComponent` (sao chép mảng MemberIDs) và `EffectsComponent` (sao chép mảng ActiveList).
    - Tối ưu hóa hiệu năng thu hồi thực thể bằng cách refactor hàm `RemoveEntity` trong [ecs.go](file:///c:/Minnsun's Adventure/server/ecs/ecs.go) từ song song 12 goroutines sang xóa tuần tự trực tiếp trên 12 component maps, loại bỏ triệt để chi phí context-switching vô ích.
    - Dọn dẹp toàn bộ comment chỉ dẫn thừa, cũ và không còn đúng thực tế tại [ecs.go](file:///c:/Minnsun's Adventure/server/ecs/ecs.go), [registry_ai.go](file:///c:/Minnsun's Adventure/server/ecs/registry_ai.go), và [inventory_component.go](file:///c:/Minnsun's Adventure/server/ecs/inventory_component.go).
    - Áp dụng gọi `.Clone()` trước mọi thay đổi trạng thái tham chiếu trong các systems: `trade.go` (giao dịch), `pickup.go` (nhặt đồ), `item_usage.go` (sử dụng vật phẩm, tự động delete item khi qty = 0), `party.go` (tổ đội), và `effects_system.go` (tác động hiệu ứng status).
    - Thêm tệp kiểm thử tích hợp [registry_race_test.go](file:///c:/Minnsun's Adventure/server/ecs/registry_race_test.go) giả lập 100 goroutines đọc ghi đồng thời trên cùng thực thể và xác minh thành công không lỗi Data Race qua cờ `-race` của Go.

## 2. Những "đặc sản" logic vừa tìm thấy (Discovered Logic Specialties)
- Cơ chế Thread-Safety của ECS dựa trên tính bất biến (Immutability) của Map/Slice trong Registry: Các luồng chỉ đọc trực tiếp, còn luồng ghi bắt buộc phải tạo bản sao mới qua `.Clone()`, thay đổi trên bản sao và ghi đè vào Registry. Điều này giúp loại bỏ hoàn toàn khóa locking thô trên Registry.
- Server sử dụng giao thức TCP thô ở cổng `:1503`.
- Dự án sử dụng Go 1.26.3, hỗ trợ đầy đủ Go Generics.
- Sử dụng mô hình ECS tách biệt tuyệt đối giữa Dữ liệu (Components) và Logic (Systems). Các thực thể chỉ tương tác qua ID kiểu `ecs.Entity` (chuỗi địa chỉ cho người chơi, hoặc ID quái vật).
- Cấu trúc thư mục Client Unity nằm dưới thư mục `client/Minnsun's Adventure` và chứa các file mã nguồn/asset cần thiết phải được theo dõi bởi Git, trong khi thư mục `Library/` chứa hàng ngàn file cache sinh ra bởi Unity.
- Các thực thể AI quái vật sẽ tự động chuyển đổi trạng thái (Idle -> Roaming -> Chasing -> Attacking -> Returning) phụ thuộc vào khoảng cách người chơi thông qua `ProximitySystem` và `SpatialGrid`.
- Database lưu trữ sử dụng MySQL (DSN local mặc định là `root:root@tcp(127.0.0.1:3306)/?parseTime=true`).
- Ràng buộc import chéo Go: Go Package không cho phép import chéo giữa hai thư mục package độc lập. Do đó, các package phụ trợ như `models` chỉ nên cung cấp dữ liệu cấu trúc thô và thao tác DB thô, không được import ngược lại package `systems`. Các logic đăng ký và tương tác hệ thống chéo phải được chuyển lên `server.go` điều phối chính.
- Giao thức lỗi nhị phân: Opcode 255 (0xFF) là opcode dành riêng cho Server Error. Client Unity cần switch trên `ErrorCode` (uint16 BE) tại byte offset 3-4 sau opcode để hiển thị popup tương ứng.

## 3. Những việc còn dang dở (Pending tasks)
- Khôi phục (Load) dữ liệu người chơi từ DB khi họ đăng nhập (hiện tại người chơi kết nối luôn được khởi tạo dưới tên Guest mới với chỉ số mặc định, chưa được load ngược lại từ các bảng `character_states` và `character_inventory`).
- Tự chạy và kiểm thử kiểm soát đa kết nối Client trong môi trường ECS mới.
- Kết nối logic Client Unity với ECS Server thông qua TCP client.
- save_engine.go — torn read
go// QueuePlayerSave: comment tự thừa nhận
// "A torn read (components from different game ticks) is acceptable"
Acceptable cho vị trí/stats, không acceptable cho inventory. Nếu player trade xong, đồng thời disconnect, inventory snapshot có thể lấy pre-trade state từ một goroutine khác đang commit. Comment che giấu một race condition thực sự.
- respawn.go — MapID hardcode
go// SpawnMonsterFromTemplate:
spawnPos := ecs.PositionComponent{MapID: 1, X: spawnX, Z: spawnZ}
Rồi RunRespawnSystem phải patch lại:
gopos.MapID = ev.MapID // "Set the correct MapID since SpawnMonsterFromTemplate defaults to 1"
Đây là workaround cho một design flaw. SpawnMonsterFromTemplate nên nhận mapID làm parameter.
- server.go — processLogin có một bug tinh vi
gocase protocol.OpcodeC2SRegister:
    // ...
    protocol.SendErrorPacket(conn, 0, "Account registered successfully!")
Dùng SendErrorPacket với errorCode = 0 để báo thành công. Client phải switch trên error code để phân biệt success/failure — fragile, và sai về mặt semantic.
- party_invite.go — AcceptPartyInviteSystem race
goif len(party.MemberIDs)+1 >= maxPartySize {  // check
    // ... gap ở đây
}
AddMemberToParty(targetPartyID, playerID)  // write
Giữa check và write, party có thể đã đầy do concurrent accept. Không có lock bao quanh toàn bộ operation.