using UnityEngine;

/// <summary>
/// Game configuration loaded from Resources/game_config.json at bootstrap.
/// Falls back to defaults if the file is missing or malformed.
/// </summary>
[System.Serializable]
public class GameConfig
{
    // ─── Network ─────────────────────────────────────────────────────────
    public string serverHost = "127.0.0.1";
    public int    serverPort = 1503;
    public string wsUrl      = "ws://127.0.0.1:8081/ws";

    // ─── Dev ─────────────────────────────────────────────────────────────
    public bool        devMode   = true;
    public string      devUsername = "test";
    public string      devPassword = "test";
    public LogLevel    logLevel  = LogLevel.Debug;

    // ─── Gameplay ────────────────────────────────────────────────────────
    public float moveSpeed      = 8f;
    public float moveSendInterval = 0.25f;
    public float heartbeatInterval = 30f;

    // ─── Camera ──────────────────────────────────────────────────────────
    public float cameraY          = 15f;
    public float cameraZ          = -10f;
    public float cameraRotX       = 45f;
    public float orthoSize        = 20f;
    public float cameraFollowSpeed = 5f;
    public float cameraMinZoom    = 5f;
    public float cameraMaxZoom    = 30f;
    public float cameraZoomSpeed  = 3f;
    public float cameraRotateSpeed = 180f;

    // ─── Entity Visual ───────────────────────────────────────────────────
    public bool usePrimitives = true; // false = use real models when available

    // ─── Rendering (procedural ground grid + lighting) ──────────────────
    public float groundSize = 1000f;
    public float groundY = -0.5f;
    public int gridCells = 100;
    public float directionalLightIntensity = 1.0f;
}

/// <summary>
/// Loads GameConfig from a JSON file in Resources/ folder.
/// Call ConfigLoader.Load() once at bootstrap.
/// </summary>
public static class ConfigLoader
{
    private const string ConfigResourcePath = "game_config";

    /// <summary>Loaded configuration — always safe to read after bootstrap.</summary>
    public static GameConfig Config { get; private set; } = new GameConfig();

    /// <summary>
    /// Load config from Resources/game_config.json.
    /// If missing or parse fails, falls back to defaults silently.
    /// Returns true if loaded successfully, false if fallback used.
    /// </summary>
    public static bool Load()
    {
        TextAsset asset = Resources.Load<TextAsset>(ConfigResourcePath);
        if (asset == null)
        {
            Debug.Log("[ConfigLoader] No config file found in Resources, using defaults");
            return false;
        }

        try
        {
            Config = JsonUtility.FromJson<GameConfig>(asset.text);
            if (Config == null)
            {
                Config = new GameConfig();
                Debug.LogWarning("[ConfigLoader] JSON parse returned null, using defaults");
                return false;
            }

            // Apply log level immediately so logging is configured early
            Logger.MinLevel = Config.logLevel;
            Logger.I("ConfigLoader", "Config loaded from Resources/{0}", ConfigResourcePath);
            return true;
        }
        catch (System.Exception ex)
        {
            Debug.LogError($"[ConfigLoader] Failed to parse config: {ex.Message}");
            Config = new GameConfig();
            return false;
        }
    }
}