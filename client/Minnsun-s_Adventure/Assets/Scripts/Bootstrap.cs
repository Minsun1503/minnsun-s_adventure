using UnityEngine;

/// <summary>
/// Entry point — creates the entire scene hierarchy in code.
/// Attach this single MonoBehaviour to an empty GameObject in an empty scene.
/// No prefabs, no inspector setup required.
/// </summary>
public class Bootstrap : MonoBehaviour
{
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
}