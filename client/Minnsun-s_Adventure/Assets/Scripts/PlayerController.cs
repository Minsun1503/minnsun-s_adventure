using UnityEngine;

/// <summary>
/// Attached to the local player's GameObject.
/// Handles input and sends movement packets to the server with throttle (250ms interval).
/// </summary>
public class PlayerController : MonoBehaviour
{
    private const float MoveSpeed = 8f;
    private const float SendInterval = 0.25f; // 4 packets/s, matches server tick rate

    private NetworkManager networkManager;
    private float sendTimer;
    private Vector3 lastSentPos;

    private void Start()
    {
        networkManager = FindObjectOfType<NetworkManager>();
    }

    private void Update()
    {
        // WASD movement — simple arrow-key input for now
        float h = Input.GetAxisRaw("Horizontal");
        float v = Input.GetAxisRaw("Vertical");

        if (h != 0 || v != 0)
        {
            Vector3 dir = new Vector3(h, 0, v).normalized;
            transform.position += dir * MoveSpeed * Time.deltaTime;

            // Throttle: only send to server every 250ms or when position changed significantly
            sendTimer += Time.deltaTime;
            if (sendTimer >= SendInterval && transform.position != lastSentPos)
            {
                SendMovePacket();
                lastSentPos = transform.position;
                sendTimer = 0;
            }
        }
    }

    private void SendMovePacket()
    {
        if (networkManager == null) return;

        int x = Mathf.RoundToInt(transform.position.x);
        int z = Mathf.RoundToInt(transform.position.z);
        byte[] payload = new byte[8];
        // Big Endian int32 x and z
        payload[0] = (byte)(x >> 24);
        payload[1] = (byte)(x >> 16);
        payload[2] = (byte)(x >> 8);
        payload[3] = (byte)x;
        payload[4] = (byte)(z >> 24);
        payload[5] = (byte)(z >> 16);
        payload[6] = (byte)(z >> 8);
        payload[7] = (byte)z;
        networkManager.SendPacket(Opcodes.C2SMove, payload);
    }
}