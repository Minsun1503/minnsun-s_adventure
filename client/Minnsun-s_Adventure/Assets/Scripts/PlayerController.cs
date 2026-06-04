using UnityEngine;

/// <summary>
/// Attached to the local player's GameObject.
/// Handles input and sends movement packets to the server.
/// </summary>
public class PlayerController : MonoBehaviour
{
    [SerializeField] private float moveSpeed = 8f;

    private NetworkManager networkManager;

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
            transform.position += dir * moveSpeed * Time.deltaTime;

            // Send movement to server
            if (networkManager != null)
            {
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
    }
}