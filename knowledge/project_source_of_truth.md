# Project Source of Truth

## 1. Những gì đã làm (What was done)
- Thêm hàm `send_notice_to_player(message string, conn net.Conn)` vào cuối file [server.go](file:///c:/Minnsun's Adventure/server/server.go) để trừu tượng hóa việc gửi gói tin/thông báo text đến client.
- Thay thế dòng ghi trực tiếp `conn.Write` chào mừng người chơi ở dòng 43 bằng lời gọi hàm `send_notice_to_player`.

## 2. Những "đặc sản" logic vừa tìm thấy (Discovered Logic Specialties)
- Server sử dụng giao thức TCP thô ở cổng `:1503`.
- Thông tin người chơi (`Player` struct) chứa ID (bằng địa chỉ remote address), Name (Guest_[4 ký tự cuối IP]), X, Z.
- Đối tượng `Player` được lưu trong map toàn cục `WorldPlayer` nhưng connection `net.Conn` không được lưu trữ trực tiếp trên struct `Player`. Do đó, khi tương tác qua connection cần truyền tham số `conn net.Conn` kèm theo.

## 3. Những việc còn dang dở (Pending tasks)
- Người dùng tự biên dịch và kiểm thử lại server (`go build`/`go run`).
