using System.Text;
using UnityEngine;

/// <summary>
/// Entry point — creates the entire scene hierarchy in code.
/// Attach this single MonoBehaviour to an empty GameObject in an empty scene.
/// No prefabs, no inspector setup required.
/// </summary>
public class Bootstrap : MonoBehaviour
{
    [Header("Dev Login (auto-send after connect)")]
    [SerializeField] private string devUsername = "test";
    [SerializeField] private string devPassword = "test";

    private NetworkManager networkManager;
    private bool loginSent;

    private void Awake()
    {
        // Persist this root GameObject across scene loads
        DontDestroyOnLoad(gameObject);

        // ── Network Layer ──
        gameObject.AddComponent<NetworkClient>();
        gameObject.AddComponent<NetworkClientWS>();
        gameObject.AddComponent<NetworkManager>();   // enables the correct transport
        gameObject.AddComponent<PacketRouter>();

        // ── Entity Root (parent for all spawned entities) ──
        GameObject entityRoot = new GameObject("EntityRoot");
        DontDestroyOnLoad(entityRoot);

        var entityManager = gameObject.AddComponent<EntityManager>();
        entityManager.EntityRoot = entityRoot.transform;

        // ── UI ──
        GameObject uiRoot = new GameObject("UIRoot");
        DontDestroyOnLoad(uiRoot);
        uiRoot.AddComponent<UIManager>();

        // ── Camera ──
        GameObject cam = new GameObject("MainCamera");
        cam.tag = "MainCamera";
        DontDestroyOnLoad(cam);
        cam.transform.position = new Vector3(0, 15, -10);
        cam.transform.rotation = Quaternion.Euler(45, 0, 0);

        Camera cameraComp = cam.AddComponent<Camera>();
        cameraComp.clearFlags = CameraClearFlags.SolidColor;
        cameraComp.backgroundColor = new Color(0.3f, 0.6f, 0.9f); // sky blue
        cameraComp.orthographic = true;
        cameraComp.orthographicSize = 20f;

        cam.AddComponent<AudioListener>();
    }

    private void Start()
    {
        // Get the NetworkManager that was just created in Awake
        networkManager = GetComponent<NetworkManager>();
        if (networkManager != null)
        {
            networkManager.OnConnected += OnTransportConnected;
            Debug.Log("[Bootstrap] Subscribed to NetworkManager.OnConnected");
        }
    }

    private void OnTransportConnected()
    {
        if (loginSent) return;
        loginSent = true;

        Debug.Log($"[Bootstrap] Connected! Sending login as '{devUsername}'...");
        byte[] payload = BuildLoginPayload(devUsername, devPassword);
        networkManager.SendPacket(Opcodes.C2SLogin, payload);
    }

    /// <summary>
    /// Build a binary login payload matching the server's parseAuthPayload format:
    /// [UsernameLen byte][Username UTF-8][PasswordLen byte][Password UTF-8]
    /// </summary>
    private static byte[] BuildLoginPayload(string username, string password)
    {
        byte[] u = Encoding.UTF8.GetBytes(username);
        byte[] p = Encoding.UTF8.GetBytes(password);
        byte[] result = new byte[2 + u.Length + p.Length];
        result[0] = (byte)u.Length;
        Buffer.BlockCopy(u, 0, result, 1, u.Length);
        result[1 + u.Length] = (byte)p.Length;
        Buffer.BlockCopy(p, 0, result, 2 + u.Length, p.Length);
        return result;
    }

    /// <summary>
    /// Clean up event subscription.
    /// </summary>
    private void OnDestroy()
    {
        if (networkManager != null)
            networkManager.OnConnected -= OnTransportConnected;
    }
}