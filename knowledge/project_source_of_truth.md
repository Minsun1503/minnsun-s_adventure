# Project Source of Truth

## 1. Những gì đã làm (What was done)
- Thêm hàm `send_notice_to_player(message string, conn net.Conn)` vào cuối file [server.go](file:///c:/Minnsun's Adventure/server/server.go) để trừu tượng hóa việc gửi gói tin/thông báo text đến client.
- Thay thế dòng ghi trực tiếp `conn.Write` chào mừng người chơi ở dòng 43 bằng lời gọi hàm `send_notice_to_player`.
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

## 2. Những "đặc sản" logic vừa tìm thấy (Discovered Logic Specialties)
- Server sử dụng giao thức TCP thô ở cổng `:1503`.
- Dự án sử dụng Go 1.26.3, hỗ trợ đầy đủ Go Generics.
- Sử dụng mô hình ECS tách biệt tuyệt đối giữa Dữ liệu (Components) và Logic (Systems). Các thực thể chỉ tương tác qua ID kiểu `ecs.Entity` (chuỗi địa chỉ cho người chơi, hoặc ID quái vật).
- Cấu trúc thư mục Client Unity nằm dưới thư mục `client/Minnsun's Adventure` và chứa các file mã nguồn/asset cần thiết phải được theo dõi bởi Git, trong khi thư mục `Library/` chứa hàng ngàn file cache sinh ra bởi Unity.
- Các thực thể AI quái vật sẽ tự động chuyển đổi trạng thái (Idle -> Roaming -> Chasing -> Attacking -> Returning) phụ thuộc vào khoảng cách người chơi thông qua `ProximitySystem` và `SpatialGrid`.

## 3. Những việc còn dang dở (Pending tasks)
- Tự chạy và kiểm thử kiểm soát đa kết nối Client trong môi trường ECS mới.
- Kết nối logic Client Unity với ECS Server thông qua TCP client.
