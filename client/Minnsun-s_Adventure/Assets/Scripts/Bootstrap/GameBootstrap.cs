using UnityEngine;

/// <summary>
/// Main entry point — bootstraps the entire client system.
/// Attach this single MonoBehaviour to an empty GameObject in an empty scene.
///
/// Initialization order:
///   1. ConfigLoader.Load() — loads game_config.json, sets Logger.MinLevel
///   2. ServiceContainer — registers core services
///   3. EventBusDispatcher — creates persistent dispatcher
///   4. Network — creates transport layer (TCP/WS), NetworkManager, PacketRouter
///   5. Entity — creates EntityRegistry + EntityService, registers in ServiceContainer
///   6. Camera & Lighting — creates CameraService, MainCamera + FollowCamera + DirectionalLight
///   7. Rendering — creates procedural ground grid (MapRenderer)
///   8. UI — creates UIRoot + UIManager, wires PacketRouter with UIManager
///   9. Auto-login — if devMode is true, sends login after connection
///
/// No prefabs, no inspector setup required. All configuration from game_config.json.
/// </summary>
public class GameBootstrap : MonoBehaviour
{
    [RuntimeInitializeOnLoadMethod(RuntimeInitializeLoadType.BeforeSceneLoad)]
    private static void AutoStart()
    {
        GameObject go = new GameObject("GameBootstrap");
        go.AddComponent<GameBootstrap>();
        // Sử dụng Debug.Log trực tiếp vì Logger.cs chưa được cấu hình LogLevel tại thời điểm BeforeSceneLoad
        Debug.Log("[Bootstrap] GameBootstrap automatically instantiated programmatically (Code-Driven AutoStart)");
    }

    private NetworkManager networkManager;

    /// <summary>Root transform for all entity GameObjects.</summary>
    private Transform entityRoot;

    // ─── Init Phases ──────────────────────────────────────────────────

    private void Awake()
    {
        DontDestroyOnLoad(gameObject);

        // Phase 1: Core Systems
        BootstrapCore();

        // Phase 2: Networking
        BootstrapNetwork();

        // Phase 3: Entity System (EntityRegistry + EntityService)
        BootstrapEntity();

        // Phase 4: Camera & Lighting
        BootstrapCamera();

        // Phase 5: Rendering (procedural ground grid)
        BootstrapRendering();

        // Phase 6: UI
        BootstrapUI();

        // Phase 7: Debug UIBridge (debug build only)
        BootstrapDebugBridge();

        // Freeze service container — no more registrations allowed
        ServiceContainer.Freeze();

        Logger.I("Bootstrap", "GameBootstrap complete — all systems initialized");
    }

    private void Start()
    {
        // Subscribe to connection event for auto-login
        EventBus<EventBusDispatcher.ConnectionEvent>.Subscribe(OnConnectionEvent);

        // Subscribe to entity spawn event for auto snapshot
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Subscribe(OnEntitySpawned);

        networkManager = ServiceContainer.Resolve<NetworkManager>();
        if (networkManager != null)
        {
            // Legacy event bridge for NetworkManager.OnConnected
            networkManager.OnConnected += OnTransportConnected;
            networkManager.OnDisconnected += OnTransportDisconnected;
            Logger.D("Bootstrap", "Subscribed to NetworkManager.OnConnected/OnDisconnected");
        }
        else
        {
            Logger.E("Bootstrap", "NetworkManager not registered in ServiceContainer");
        }
    }

    // ─── Phase 1: Core ────────────────────────────────────────────────

    private void BootstrapCore()
    {
        Logger.I("Bootstrap", "Phase 1: Loading config and initializing core systems");

        // Load config (also sets Logger.MinLevel)
        bool hasConfig = ConfigLoader.Load();

        // Register core services
        ServiceContainer.Register(ConfigLoader.Config);

        if (!hasConfig)
        {
            Logger.W("Bootstrap", "No config file found — using hardcoded defaults");
        }

        Logger.D("Bootstrap", "Config loaded. LogLevel={0}", ConfigLoader.Config.logLevel);

        // Create persistent event bus dispatcher
        gameObject.AddComponent<EventBusDispatcher>();
        Logger.D("Bootstrap", "EventBusDispatcher created");
    }

    // ─── Phase 2: Network ─────────────────────────────────────────────

    private void BootstrapNetwork()
    {
        Logger.I("Bootstrap", "Phase 2: Initializing network layer");

        GameConfig cfg = ConfigLoader.Config;

        // Create transport components
        NetworkClient tcpClient = gameObject.AddComponent<NetworkClient>();
        NetworkClientWS wsClient = gameObject.AddComponent<NetworkClientWS>();
        networkManager = gameObject.AddComponent<NetworkManager>();
        PacketRouter router = gameObject.AddComponent<PacketRouter>();

        // Register in ServiceContainer for other systems to resolve
        ServiceContainer.Register(tcpClient);
        ServiceContainer.Register(wsClient);
        ServiceContainer.Register(networkManager);
        ServiceContainer.Register(router);

        // Override transport with config values
        tcpClient.SetHost(cfg.serverHost, cfg.serverPort);
        wsClient.SetUrl(cfg.wsUrl);

        Logger.D("Bootstrap", "Network components registered. TCP={0}:{1}, WS={2}",
            cfg.serverHost, cfg.serverPort, cfg.wsUrl);
    }

    // ─── Phase 3: Entity ──────────────────────────────────────────────

    private void BootstrapEntity()
    {
        Logger.I("Bootstrap", "Phase 3: Creating entity system (EntityRegistry + EntityService)");

        // Create EntityRoot parent transform for all entity GameObjects
        GameObject entityRootGO = new GameObject("EntityRoot");
        DontDestroyOnLoad(entityRootGO);
        entityRoot = entityRootGO.transform;

        // Create pure C# EntityRegistry singleton and register in ServiceContainer
        EntityRegistry registry = new EntityRegistry();
        ServiceContainer.Register(registry);
        Logger.D("Bootstrap", "EntityRegistry registered in ServiceContainer");

        // Create EntityService MonoBehaviour on this GameObject
        EntityService entityService = gameObject.AddComponent<EntityService>();
        // UIManager not yet created; EntityService will resolve it later via ServiceContainer
        entityService.Init(registry, null, entityRoot);
        Logger.D("Bootstrap", "EntityService created and initialized");
    }

    // ─── Phase 4: Camera & Lighting ───────────────────────────────────

    private void BootstrapCamera()
    {
        Logger.I("Bootstrap", "Phase 4: Creating camera with FollowCamera, CameraService, and DirectionalLight");

        GameConfig cfg = ConfigLoader.Config;

        // ── CameraService (pure C# singleton) ──────────────────────────
        CameraService cameraService = new CameraService();
        ServiceContainer.Register(cameraService);
        Logger.D("Bootstrap", "CameraService registered in ServiceContainer");

        // ── Camera ────────────────────────────────────────────────────
        // Disable existing scene cameras (like the default main camera) to prevent AudioListener warning spams
        var existingCams = FindObjectsByType<Camera>(FindObjectsSortMode.None);
        foreach (var c in existingCams)
        {
            c.gameObject.SetActive(false);
            Logger.D("Bootstrap", "Disabled existing camera '{0}' to avoid conflicts", c.name);
        }

        GameObject cam = new GameObject("MainCamera");
        cam.tag = "MainCamera";
        DontDestroyOnLoad(cam);
        cam.transform.position = new Vector3(0, cfg.cameraY, cfg.cameraZ);
        cam.transform.rotation = Quaternion.Euler(cfg.cameraRotX, 0, 0);

        Camera cameraComp = cam.AddComponent<Camera>();
        cameraComp.clearFlags = CameraClearFlags.SolidColor;
        cameraComp.backgroundColor = new Color(0.3f, 0.6f, 0.9f);
        cameraComp.orthographic = true;
        cameraComp.orthographicSize = cfg.orthoSize;

        cam.AddComponent<AudioListener>();

        // Attach FollowCamera (replaces old CameraFollow)
        // Will be configured by PlayerView via CameraService when local player spawns
        FollowCamera followCamera = cam.AddComponent<FollowCamera>();
        followCamera.baseOffset = new Vector3(0, cfg.cameraY, cfg.cameraZ);
        followCamera.followSpeed = cfg.cameraFollowSpeed;
        followCamera.minZoom = cfg.cameraMinZoom;
        followCamera.maxZoom = cfg.cameraMaxZoom;
        followCamera.zoomSpeed = cfg.cameraZoomSpeed;
        followCamera.rotateSpeed = cfg.cameraRotateSpeed;

        // Wire CameraService to FollowCamera
        cameraService.Init(followCamera);

        // ── Directional Light ─────────────────────────────────────────
        GameObject lightGO = new GameObject("DirectionalLight");
        DontDestroyOnLoad(lightGO);
        lightGO.transform.rotation = Quaternion.Euler(50f, -30f, 0f);

        Light dLight = lightGO.AddComponent<Light>();
        dLight.type = LightType.Directional;
        dLight.intensity = 1.0f;
        dLight.shadows = LightShadows.Soft;
        dLight.shadowStrength = 0.8f;
        dLight.shadowBias = 0.05f;
        dLight.shadowNormalBias = 0.4f;
        dLight.color = Color.white;

        Logger.D("Bootstrap", "Camera created at ({0}, {1}, {2}) orthoSize={3} with FollowCamera + CameraService",
            0, cfg.cameraY, cfg.cameraZ, cfg.orthoSize);
        Logger.D("Bootstrap", "DirectionalLight created at rotation (50, -30, 0) intensity={0}", dLight.intensity);
    }

    // ─── Phase 5: Rendering ──────────────────────────────────────────

    private void BootstrapRendering()
    {
        Logger.I("Bootstrap", "Phase 5: Creating procedural ground grid (MapRenderer)");

        GameConfig cfg = ConfigLoader.Config;

        GameObject groundRoot = new GameObject("GroundRoot");
        DontDestroyOnLoad(groundRoot);

        MapRenderer mapRenderer = groundRoot.AddComponent<MapRenderer>();
        mapRenderer.groundSize = cfg.groundSize;
        mapRenderer.groundY = cfg.groundY;
        mapRenderer.gridCells = cfg.gridCells;
        mapRenderer.Build();

        Logger.D("Bootstrap", "MapRenderer initialized — ground grid created");
    }

    // ─── Phase 6: UI ──────────────────────────────────────────────────

    private void BootstrapUI()
    {
        Logger.I("Bootstrap", "Phase 6: Creating UI");

        // Create UIRoot with UIManager
        GameObject uiRoot = new GameObject("UIRoot");
        DontDestroyOnLoad(uiRoot);
        UIManager uiManager = uiRoot.AddComponent<UIManager>();
        ServiceContainer.Register(uiManager);

        // Wire PacketRouter with UIManager reference (no EntityManager needed)
        var router = ServiceContainer.Resolve<PacketRouter>();
        if (router != null && uiManager != null)
        {
            router.Init(uiManager);
            Logger.D("Bootstrap", "PacketRouter wired to UIManager");
        }

        // Wire EntityService with UIManager now that it exists
        var entityService = GetComponent<EntityService>();
        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (entityService != null && registry != null && uiManager != null)
        {
            entityService.Init(registry, uiManager, entityRoot);
            Logger.D("Bootstrap", "EntityService re-initialized with UIManager reference");
        }

        // Create VirtualJoystick (persists across scenes)
        // Must add Canvas BEFORE VirtualJoystick so Awake can find it via GetComponent<Canvas>()
        GameObject joystickGO = new GameObject("VirtualJoystick_Canvas");
        DontDestroyOnLoad(joystickGO);
        joystickGO.AddComponent<Canvas>();
        joystickGO.AddComponent<VirtualJoystick>();
        Logger.D("Bootstrap", "VirtualJoystick created");
    }

    // ─── Phase 7: Debug UIBridge ────────────────────────────────────────

    private void BootstrapDebugBridge()
    {
        if (!Debug.isDebugBuild)
        {
            Logger.D("Bootstrap", "Skipping UIBridge (not a debug build)");
            return;
        }

        Logger.I("Bootstrap", "Phase 7: Initializing UIBridge + MainThreadDispatcher (debug build)");

        // MainThreadDispatcher singleton — created on demand, but ensure it exists early
        var mtd = MainThreadDispatcher.Instance;
        if (mtd != null)
        {
            Logger.D("Bootstrap", "MainThreadDispatcher ready");
        }

        // UIBridge — TCP listener on port 13000
        gameObject.AddComponent<UIBridge>();
        Logger.D("Bootstrap", "UIBridge added to root GameObject");
    }

    // ─── Auto-Login ───────────────────────────────────────────────────

    /// <summary>
    /// Called via EventBus when connection state changes.
    /// </summary>
    private void OnConnectionEvent(EventBusDispatcher.ConnectionEvent evt)
    {
        if (evt.Connected && ConfigLoader.Config.devMode)
        {
            AutoLogin();
        }
    }

    /// <summary>
    /// Legacy handler — called from NetworkManager.OnConnected.
    /// Still needed because NetworkClient currently fires OnConnected directly.
    /// </summary>
    private void OnTransportConnected()
    {
        // Update status indicator
        var ui = ServiceContainer.Resolve<UIManager>();
        if (ui != null)
            ui.UpdateConnectionStatus("Connected", Color.green);

        // Publish event for EventBus subscribers
        EventBus<EventBusDispatcher.ConnectionEvent>.Publish(
            new EventBusDispatcher.ConnectionEvent { Connected = true });

        if (ConfigLoader.Config.devMode)
        {
            AutoLogin();
        }
    }

    /// <summary>
    /// Called when the transport disconnects — reset login flag for reconnect auto-login.
    /// Also stops auto snapshot if running.
    /// </summary>
    private void OnTransportDisconnected()
    {
        ClientSnapshotDumper.StopAutoSnapshot();

        var ui = ServiceContainer.Resolve<UIManager>();
        if (ui != null)
            ui.UpdateConnectionStatus("Disconnected", Color.gray);
        Logger.W("Bootstrap", "Transport disconnected");
    }

    /// <summary>
    /// Called when an entity spawns. If it's the local player, start auto snapshots.
    /// </summary>
    private void OnEntitySpawned(EventBusDispatcher.EntitySpawnedEvent evt)
    {
        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (registry != null && registry.LocalPlayerID == evt.EntityID)
        {
            Logger.I("Bootstrap", "Local player spawned — starting auto snapshots (2s interval)");
            ClientSnapshotDumper.StartAutoSnapshot(2f);
        }
    }

    private void AutoLogin()
    {
        // AutoLogin disabled. Using LoginUI instead.
        if (gameObject.GetComponent<LoginUI>() == null)
        {
            gameObject.AddComponent<LoginUI>();
        }
    }

    // ─── Cleanup ──────────────────────────────────────────────────────

    private void OnDestroy()
    {
        EventBus<EventBusDispatcher.ConnectionEvent>.Unsubscribe(OnConnectionEvent);
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Unsubscribe(OnEntitySpawned);

        ClientSnapshotDumper.StopAutoSnapshot();

        if (networkManager != null)
        {
            networkManager.OnConnected -= OnTransportConnected;
            networkManager.OnDisconnected -= OnTransportDisconnected;
        }

        ServiceContainer.Clear();
        Logger.I("Bootstrap", "GameBootstrap destroyed — services cleared");
    }
}