using UnityEngine;
using UnityEngine.UI;
using UnityEngine.EventSystems;
using UnityEngine.InputSystem.UI;

/// <summary>
/// Procedural virtual joystick for touch/mouse input.
/// Creates its own Canvas overlay, Background circle, Handle circle, and Attack button.
/// Auto-creates EventSystem + InputSystemUIInputModule if none exists in the scene.
///
/// Singleton: use VirtualJoystick.Instance for public access.
/// Provides InputDirection (normalized, clamped to unit circle) and IsAttackPressed.
/// </summary>
[RequireComponent(typeof(Canvas))]
public class VirtualJoystick : MonoBehaviour, IDragHandler, IPointerDownHandler, IPointerUpHandler
{
    // ─── Singleton ────────────────────────────────────────────────────────
    public static VirtualJoystick Instance { get; private set; }

    // ─── Public State ─────────────────────────────────────────────────────
    /// <summary>Normalized input direction (clamped magnitude <= 1).</summary>
    public Vector2 InputDirection { get; private set; }

    /// <summary>True on the frame the attack button was pressed.</summary>
    public bool IsAttackPressed { get; private set; }

    // ─── Config ───────────────────────────────────────────────────────────
    private const float BackgroundRadius = 60f;
    private const float HandleRadius = 25f;
    private const float BackgroundAlpha = 0.3f;
    private const float HandleAlpha = 0.6f;

    // ─── References ───────────────────────────────────────────────────────
    private RectTransform backgroundRT;
    private RectTransform handleRT;
    private Vector2 joystickCenter;
    private float backgroundRadiusPx;

    // ─── Lifecycle ────────────────────────────────────────────────────────

    private void Awake()
    {
        // Singleton enforcement
        if (Instance != null && Instance != this)
        {
            Destroy(gameObject);
            return;
        }
        Instance = this;
        DontDestroyOnLoad(gameObject);

        // Ensure EventSystem exists (required for UI input events)
        EnsureEventSystem();

        // Create the Canvas and joystick hierarchy
        CreateJoystickCanvas();
    }

    private void OnDestroy()
    {
        if (Instance == this)
            Instance = null;
    }

    private void LateUpdate()
    {
        // Reset per-frame attack flag (set to true only on press frame)
        IsAttackPressed = false;
    }

    // ─── EventSystem Guard ────────────────────────────────────────────────

    /// <summary>
    /// Auto-create EventSystem + InputSystemUIInputModule if not present.
    /// Required for IDragHandler/IPointerDownHandler to function.
    /// </summary>
    private static void EnsureEventSystem()
    {
        if (FindFirstObjectByType<EventSystem>() != null)
            return;

        GameObject esGO = new GameObject("EventSystem");
        esGO.AddComponent<EventSystem>();
        esGO.AddComponent<InputSystemUIInputModule>();
        Object.DontDestroyOnLoad(esGO);
        Debug.Log("[VirtualJoystick] Created EventSystem + InputSystemUIInputModule");
    }

    // ─── Canvas Creation ─────────────────────────────────────────────────

    private void CreateJoystickCanvas()
    {
        // ── Root Canvas ──
        gameObject.name = "VirtualJoystick_Canvas";
        Canvas canvas = GetComponent<Canvas>();
        canvas.renderMode = RenderMode.ScreenSpaceOverlay;
        canvas.sortingOrder = 100; // above UIManager's canvas

        CanvasScaler scaler = gameObject.AddComponent<CanvasScaler>();
        scaler.uiScaleMode = CanvasScaler.ScaleMode.ScaleWithScreenSize;
        scaler.referenceResolution = new Vector2(1920, 1080);

        gameObject.AddComponent<GraphicRaycaster>();

        // ── Joystick Container (bottom-left) ──
        GameObject joyContainer = new GameObject("JoystickContainer");
        joyContainer.transform.SetParent(transform, false);
        RectTransform containerRT = joyContainer.AddComponent<RectTransform>();
        containerRT.anchorMin = new Vector2(0, 0);
        containerRT.anchorMax = new Vector2(0, 0);
        containerRT.pivot = new Vector2(0.5f, 0.5f);
        containerRT.anchoredPosition = new Vector2(BackgroundRadius + 20f, BackgroundRadius + 20f);
        containerRT.sizeDelta = new Vector2(BackgroundRadius * 2, BackgroundRadius * 2);

        // ── Background Circle ──
        GameObject bgGO = new GameObject("JoystickBackground");
        bgGO.transform.SetParent(containerRT.transform, false);
        backgroundRT = bgGO.AddComponent<RectTransform>();
        backgroundRT.anchorMin = Vector2.zero;
        backgroundRT.anchorMax = Vector2.one;
        backgroundRT.offsetMin = Vector2.zero;
        backgroundRT.offsetMax = Vector2.zero;

        Image bgImage = bgGO.AddComponent<Image>();
        bgImage.sprite = CreateCircleSprite((int)(BackgroundRadius * 2), Color.white);
        bgImage.color = new Color(1, 1, 1, BackgroundAlpha);
        bgImage.raycastTarget = true;

        // ── Handle Circle ──
        GameObject handleGO = new GameObject("JoystickHandle");
        handleGO.transform.SetParent(containerRT.transform, false);
        handleRT = handleGO.AddComponent<RectTransform>();
        handleRT.anchorMin = new Vector2(0.5f, 0.5f);
        handleRT.anchorMax = new Vector2(0.5f, 0.5f);
        handleRT.pivot = new Vector2(0.5f, 0.5f);
        handleRT.sizeDelta = new Vector2(HandleRadius * 2, HandleRadius * 2);
        handleRT.anchoredPosition = Vector2.zero;

        Image handleImage = handleGO.AddComponent<Image>();
        handleImage.sprite = CreateCircleSprite((int)(HandleRadius * 2), Color.white);
        handleImage.color = new Color(1, 1, 1, HandleAlpha);
        handleImage.raycastTarget = false; // handle doesn't need raycast — background does

        // Add pointer handlers to the container (background receives drag/click)
        bgGO.AddComponent<JoystickInputReceiver>().Init(this);

        // Store center and radius for input normalization
        joystickCenter = Vector2.zero;
        backgroundRadiusPx = BackgroundRadius;

        // ── Attack Button ──
        CreateAttackButton(containerRT);
    }

    // ─── Attack Button ───────────────────────────────────────────────────

    private void CreateAttackButton(RectTransform joystickContainer)
    {
        GameObject atkGO = new GameObject("AttackButton");
        atkGO.transform.SetParent(transform, false);
        RectTransform atkRT = atkGO.AddComponent<RectTransform>();
        atkRT.anchorMin = new Vector2(0, 0);
        atkRT.anchorMax = new Vector2(0, 0);
        atkRT.pivot = new Vector2(0.5f, 0.5f);
        // Place to the right of joystick
        float joystickRightEdge = BackgroundRadius + 20f + BackgroundRadius;
        atkRT.anchoredPosition = new Vector2(joystickRightEdge + 60f, BackgroundRadius + 20f);
        atkRT.sizeDelta = new Vector2(80, 80);

        Image atkImage = atkGO.AddComponent<Image>();
        atkImage.sprite = CreateCircleSprite(80, Color.white);
        atkImage.color = new Color(1, 0.3f, 0.3f, 0.6f); // semi-transparent red
        atkImage.raycastTarget = true;

        // Add AttackButtonHandler
        AttackButtonHandler handler = atkGO.AddComponent<AttackButtonHandler>();
        handler.joystick = this;
    }

    // ─── Input Handlers (delegated from JoystickInputReceiver) ────────────

    public void OnPointerDown(PointerEventData eventData)
    {
        OnDrag(eventData);
    }

    public void OnDrag(PointerEventData eventData)
    {
        // Convert screen position to local position within the container
        Vector2 localPos;
        RectTransformUtility.ScreenPointToLocalPointInRectangle(
            backgroundRT, eventData.position, eventData.pressEventCamera, out localPos);

        // Clamp to background circle
        float distance = localPos.magnitude;
        float maxDist = backgroundRadiusPx - HandleRadius;
        if (distance > maxDist)
        {
            localPos = localPos.normalized * maxDist;
        }

        // Move handle
        handleRT.anchoredPosition = localPos;

        // Compute normalized input direction (unit circle)
        if (maxDist > 0.01f)
        {
            InputDirection = new Vector2(localPos.x / maxDist, localPos.y / maxDist);
            float mag = InputDirection.magnitude;
            if (mag > 1f)
                InputDirection /= mag;
        }
        else
        {
            InputDirection = Vector2.zero;
        }
    }

    public void OnPointerUp(PointerEventData eventData)
    {
        // Reset handle to center
        handleRT.anchoredPosition = Vector2.zero;
        InputDirection = Vector2.zero;
    }

    /// <summary>Called by AttackButtonHandler on pointer down.</summary>
    public void OnAttackPressed()
    {
        IsAttackPressed = true;
    }

    // ─── Procedural Circle Sprite ────────────────────────────────────────

    /// <summary>
    /// Generates a simple white circle texture and wraps it in a Sprite.
    /// Cached to avoid generating multiple times.
    /// </summary>
    private static Sprite CreateCircleSprite(int size, Color color)
    {
        // Use a simple linear texture approach
        Texture2D tex = new Texture2D(size, size, TextureFormat.RGBA32, false);
        tex.filterMode = FilterMode.Bilinear;

        float radius = size / 2f;
        float radiusSq = radius * radius;
        Color[] pixels = new Color[size * size];

        for (int y = 0; y < size; y++)
        {
            for (int x = 0; x < size; x++)
            {
                float dx = x - radius + 0.5f;
                float dy = y - radius + 0.5f;
                float distSq = dx * dx + dy * dy;
                // Anti-aliased edge: smooth step
                float alpha = Mathf.Clamp01((radiusSq - distSq) / (radius * 2f));
                pixels[y * size + x] = new Color(color.r, color.g, color.b, alpha);
            }
        }

        tex.SetPixels(pixels);
        tex.Apply();

        return Sprite.Create(tex, new Rect(0, 0, size, size), new Vector2(0.5f, 0.5f), 100f);
    }
}

// ─── Helper: JoystickInputReceiver ───────────────────────────────────────

/// <summary>
/// Attached to the background Image to forward pointer events to VirtualJoystick.
/// </summary>
public class JoystickInputReceiver : MonoBehaviour, IDragHandler, IPointerDownHandler, IPointerUpHandler
{
    private VirtualJoystick joystick;

    public void Init(VirtualJoystick j) { joystick = j; }

    public void OnPointerDown(PointerEventData eventData) => joystick?.OnPointerDown(eventData);
    public void OnDrag(PointerEventData eventData) => joystick?.OnDrag(eventData);
    public void OnPointerUp(PointerEventData eventData) => joystick?.OnPointerUp(eventData);
}

// ─── Helper: AttackButtonHandler ─────────────────────────────────────────

/// <summary>
/// Attached to the Attack button to forward pointer-down to VirtualJoystick.
/// </summary>
public class AttackButtonHandler : MonoBehaviour, IPointerDownHandler
{
    public VirtualJoystick joystick;

    public void OnPointerDown(PointerEventData eventData)
    {
        joystick?.OnAttackPressed();
    }
}