using UnityEngine;
using UnityEngine.UI;
using TMPro;

/// <summary>
/// Manages all in-game UI: graphical HUD (HP/MP bars, Level, FPS, Ping, Coordinates),
/// chat box, combat damage numbers, notices.
/// All UI elements are created in code using TextMeshPro and Image components (no prefabs).
/// </summary>
public class UIManager : MonoBehaviour
{
    // ─── HUD Elements ─────────────────────────────────────────────────────
    private RectTransform hpBarFill;
    private RectTransform mpBarFill;
    private TextMeshProUGUI hpText;
    private TextMeshProUGUI mpText;
    private TextMeshProUGUI levelText;
    private TextMeshProUGUI perfText; // FPS / Ping / Coordinates

    private TextMeshProUGUI noticeText;
    private TextMeshProUGUI chatText;

    // ─── Chat buffer ─────────────────────────────────────────────────────
    private readonly System.Collections.Generic.List<string> chatLines =
        new System.Collections.Generic.List<string>();
    private const int MaxChatLines = 50;

    // ─── Connection Status ──────────────────────────────────────────────
    private TextMeshProUGUI statusText;
    private string lastStatus = "";

    // ─── FPS Calculation ────────────────────────────────────────────────
    private const int FpsSamples = 20;
    private readonly float[] fpsDeltaHistory = new float[FpsSamples];
    private int fpsSampleIndex;

    // ─── Color Constants ────────────────────────────────────────────────
    private static readonly Color BgBarColor = new Color(0.15f, 0.15f, 0.15f, 0.8f);
    private static readonly Color HpBarColor = new Color(0.22f, 0.78f, 0.22f, 1f);   // green
    private static readonly Color HpBarDamageColor = new Color(0.85f, 0.2f, 0.2f, 1f); // red when low
    private static readonly Color MpBarColor = new Color(0.2f, 0.5f, 1f, 1f);        // blue

    private void Awake()
    {
        CreateCanvas();
    }

    private void Start()
    {
        // Subscribe to EventBus for stats and combat updates
        EventBus<EventBusDispatcher.StatsUpdatedEvent>.Subscribe(OnStatsUpdated);
        EventBus<EventBusDispatcher.CombatHitEvent>.Subscribe(OnCombatHitReceived);
        Logger.D("UIManager", "Subscribed to EventBus events");
    }

    private void OnDestroy()
    {
        EventBus<EventBusDispatcher.StatsUpdatedEvent>.Unsubscribe(OnStatsUpdated);
        EventBus<EventBusDispatcher.CombatHitEvent>.Unsubscribe(OnCombatHitReceived);
    }

    private void Update()
    {
        UpdateFpsCounter();
        UpdatePerfPanel();
    }

    // ─── FPS Calculator ──────────────────────────────────────────────────

    /// <summary>
    /// Rolling-average FPS calculation from Time.deltaTime samples.
    /// </summary>
    private void UpdateFpsCounter()
    {
        fpsDeltaHistory[fpsSampleIndex] = Time.unscaledDeltaTime;
        fpsSampleIndex = (fpsSampleIndex + 1) % FpsSamples;
    }

    /// <summary>
    /// Compute average FPS over the last N samples.
    /// </summary>
    private float GetAverageFps()
    {
        float sum = 0f;
        for (int i = 0; i < FpsSamples; i++)
            sum += fpsDeltaHistory[i];
        float avgDelta = sum / FpsSamples;
        return avgDelta > 0f ? 1f / avgDelta : 0f;
    }

    // ─── Performance Panel Update ────────────────────────────────────────

    private void UpdatePerfPanel()
    {
        if (perfText == null) return;

        // FPS
        float fps = GetAverageFps();
        string fpsStr = $"{fps:F1}";

        // Ping
        float ping = 0f;
        var netManager = ServiceContainer.Resolve<NetworkManager>();
        if (netManager != null)
            ping = netManager.LastPingMs;

        // Coordinates from local player
        string coordStr = "-, -";
        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (registry != null)
        {
            EntityView localView = registry.Get(registry.LocalPlayerID);
            if (localView != null)
            {
                Vector3 pos = localView.transform.position;
                coordStr = $"{pos.x:F0}, {pos.z:F0}";
            }
        }

        perfText.text = $"FPS: {fpsStr}\nPing: {ping:F0}ms\nPos: {coordStr}";
    }

    // ─── Event Handlers ──────────────────────────────────────────────────

    /// <summary>
    /// Handle StatsUpdatedEvent from EventBus.
    /// If the stats belong to the local player, refresh the HUD.
    /// </summary>
    private void OnStatsUpdated(EventBusDispatcher.StatsUpdatedEvent evt)
    {
        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (registry == null) return;

        if (evt.EntityID == registry.LocalPlayerID)
        {
            EntityView view = registry.Get(evt.EntityID);
            if (view != null)
            {
                var statsPacket = new Decoders.StatsPacket
                {
                    EntityID = evt.EntityID,
                    HP = view.HP,
                    MaxHP = view.MaxHP,
                    MP = view.MP,
                    MaxMP = view.MaxMP,
                    Dam = view.Dam,
                    Level = view.Level
                };
                UpdateHUD(statsPacket);
            }
        }
    }

    /// <summary>
    /// Handle CombatHitEvent from EventBus — show damage number.
    /// </summary>
    private void OnCombatHitReceived(EventBusDispatcher.CombatHitEvent evt)
    {
        var combatPacket = new Decoders.CombatHitPacket
        {
            AttackerID = evt.AttackerID,
            TargetID = evt.TargetID,
            Damage = evt.Damage,
            TargetHP = 0,
            Killed = evt.Killed
        };

        var registry = ServiceContainer.Resolve<EntityRegistry>();
        if (registry != null)
        {
            EntityView targetView = registry.Get(evt.TargetID);
            if (targetView != null)
                combatPacket.TargetHP = targetView.HP;
        }

        ShowDamageNumber(combatPacket);
    }

    // ─── Canvas Creation ─────────────────────────────────────────────────

    /// <summary>
    /// Create the entire UI hierarchy in code — no prefabs needed.
    /// Layout:
    ///   Top-Left: HUD Panel (HP bar, MP bar, Level text)
    ///             Performance Panel (FPS, Ping, Coordinates) below HUD
    ///   Top-Right: Connection Status
    ///   Center:    Notice
    ///   Bottom-Left: Chat Box
    /// </summary>
    private void CreateCanvas()
    {
        // Root Canvas
        GameObject canvasGO = new GameObject("UI_Canvas");
        Canvas canvas = canvasGO.AddComponent<Canvas>();
        canvas.renderMode = RenderMode.ScreenSpaceOverlay;
        canvasGO.AddComponent<CanvasScaler>();
        canvasGO.AddComponent<GraphicRaycaster>();

        // ──────────────── TOP-LEFT: HUD Panel ───────────────────────────────
        CreateHudPanel(canvasGO.transform);

        // ──────────────── TOP-LEFT: Performance Panel (below HUD) ────────────
        CreatePerfPanel(canvasGO.transform);

        // ──────────────── TOP-RIGHT: Connection Status ───────────────────────
        CreateStatusPanel(canvasGO.transform);

        // ──────────────── CENTER: Notice ─────────────────────────────────────
        CreateNoticePanel(canvasGO.transform);

        // ──────────────── BOTTOM-LEFT: Chat Box ─────────────────────────────
        CreateChatPanel(canvasGO.transform);

        // ──────────────── Damage Number parent ──────────────────────────────
        GameObject dmgRoot = new GameObject("DamageRoot");
        dmgRoot.transform.SetParent(canvasGO.transform, false);
    }

    // ─── HUD Panel ────────────────────────────────────────────────────────

    /// <summary>
    /// Creates the graphical HUD with HP bar, MP bar, and Level text.
    /// All bars are created programmatically using UI Images.
    /// </summary>
    private void CreateHudPanel(Transform canvasTransform)
    {
        // ── HUD Container ──
        GameObject hudGO = new GameObject("HUD_Panel");
        hudGO.transform.SetParent(canvasTransform, false);
        RectTransform hudRT = hudGO.AddComponent<RectTransform>();
        hudRT.anchorMin = new Vector2(0, 1);
        hudRT.anchorMax = new Vector2(0, 1);
        hudRT.pivot = new Vector2(0, 1);
        hudRT.anchoredPosition = new Vector2(12, -12);
        hudRT.sizeDelta = new Vector2(240, 100);

        // ── HP Bar Background ──
        GameObject hpBgGO = new GameObject("HP_Bar_Background");
        hpBgGO.transform.SetParent(hudGO.transform, false);
        RectTransform hpBgRT = hpBgGO.AddComponent<RectTransform>();
        hpBgRT.anchorMin = new Vector2(0, 0.65f);
        hpBgRT.anchorMax = new Vector2(1, 0.95f);
        hpBgRT.offsetMin = Vector2.zero;
        hpBgRT.offsetMax = Vector2.zero;
        Image hpBgImg = hpBgGO.AddComponent<Image>();
        hpBgImg.color = BgBarColor;

        // ── HP Bar Fill ──
        GameObject hpFillGO = new GameObject("HP_Bar_Fill");
        hpFillGO.transform.SetParent(hpBgGO.transform, false);
        hpBarFill = hpFillGO.AddComponent<RectTransform>();
        hpBarFill.anchorMin = Vector2.zero;
        hpBarFill.anchorMax = Vector2.one; // start full, UpdateHUD sets percentage
        hpBarFill.offsetMin = Vector2.zero;
        hpBarFill.offsetMax = Vector2.zero;
        Image hpFillImg = hpFillGO.AddComponent<Image>();
        hpFillImg.color = HpBarColor;
        hpFillImg.type = Image.Type.Filled;
        hpFillImg.fillMethod = Image.FillMethod.Horizontal;
        hpFillImg.fillAmount = 1f;

        // ── HP Text (overlay on bar) ──
        GameObject hpTextGO = new GameObject("HP_Text");
        hpTextGO.transform.SetParent(hpBgGO.transform, false);
        RectTransform hpTextRT = hpTextGO.AddComponent<RectTransform>();
        hpTextRT.anchorMin = Vector2.zero;
        hpTextRT.anchorMax = Vector2.one;
        hpTextRT.offsetMin = Vector2.zero;
        hpTextRT.offsetMax = Vector2.zero;
        hpText = MakeTextMeshPro(hpTextGO, "HP: - / -");
        hpText.alignment = TextAlignmentOptions.Center;
        hpText.fontSize = 14;
        hpText.color = Color.white;
        hpText.fontStyle = FontStyles.Bold;

        // ── MP Bar Background ──
        GameObject mpBgGO = new GameObject("MP_Bar_Background");
        mpBgGO.transform.SetParent(hudGO.transform, false);
        RectTransform mpBgRT = mpBgGO.AddComponent<RectTransform>();
        mpBgRT.anchorMin = new Vector2(0, 0.30f);
        mpBgRT.anchorMax = new Vector2(1, 0.60f);
        mpBgRT.offsetMin = Vector2.zero;
        mpBgRT.offsetMax = Vector2.zero;
        Image mpBgImg = mpBgGO.AddComponent<Image>();
        mpBgImg.color = BgBarColor;

        // ── MP Bar Fill ──
        GameObject mpFillGO = new GameObject("MP_Bar_Fill");
        mpFillGO.transform.SetParent(mpBgGO.transform, false);
        mpBarFill = mpFillGO.AddComponent<RectTransform>();
        mpBarFill.anchorMin = Vector2.zero;
        mpBarFill.anchorMax = Vector2.one; // start full
        mpBarFill.offsetMin = Vector2.zero;
        mpBarFill.offsetMax = Vector2.zero;
        Image mpFillImg = mpFillGO.AddComponent<Image>();
        mpFillImg.color = MpBarColor;
        mpFillImg.type = Image.Type.Filled;
        mpFillImg.fillMethod = Image.FillMethod.Horizontal;
        mpFillImg.fillAmount = 1f;

        // ── MP Text (overlay on bar) ──
        GameObject mpTextGO = new GameObject("MP_Text");
        mpTextGO.transform.SetParent(mpBgGO.transform, false);
        RectTransform mpTextRT = mpTextGO.AddComponent<RectTransform>();
        mpTextRT.anchorMin = Vector2.zero;
        mpTextRT.anchorMax = Vector2.one;
        mpTextRT.offsetMin = Vector2.zero;
        mpTextRT.offsetMax = Vector2.zero;
        mpText = MakeTextMeshPro(mpTextGO, "MP: - / -");
        mpText.alignment = TextAlignmentOptions.Center;
        mpText.fontSize = 14;
        mpText.color = Color.white;
        mpText.fontStyle = FontStyles.Bold;

        // ── Level Text ──
        GameObject lvlGO = new GameObject("Level_Text");
        lvlGO.transform.SetParent(hudGO.transform, false);
        RectTransform lvlRT = lvlGO.AddComponent<RectTransform>();
        lvlRT.anchorMin = new Vector2(0, 0);
        lvlRT.anchorMax = new Vector2(1, 0.25f);
        lvlRT.offsetMin = Vector2.zero;
        lvlRT.offsetMax = Vector2.zero;
        levelText = MakeTextMeshPro(lvlGO, "Lv: -");
        levelText.alignment = TextAlignmentOptions.Left;
        levelText.fontSize = 13;
        levelText.color = new Color(0.8f, 0.8f, 0.3f); // gold-ish
    }

    // ─── Performance Panel ────────────────────────────────────────────────

    /// <summary>
    /// Performance panel showing FPS, Ping, and Coordinates.
    /// Placed below the HUD panel.
    /// </summary>
    private void CreatePerfPanel(Transform canvasTransform)
    {
        GameObject perfGO = new GameObject("Performance_Panel");
        perfGO.transform.SetParent(canvasTransform, false);
        RectTransform perfRT = perfGO.AddComponent<RectTransform>();
        perfRT.anchorMin = new Vector2(0, 1);
        perfRT.anchorMax = new Vector2(0, 1);
        perfRT.pivot = new Vector2(0, 1);
        perfRT.anchoredPosition = new Vector2(12, -120); // below HUD (100 height + gap)
        perfRT.sizeDelta = new Vector2(180, 60);

        perfText = MakeTextMeshPro(perfGO, "FPS: -\nPing: -ms\nPos: -, -");
        perfText.alignment = TextAlignmentOptions.TopLeft;
        perfText.fontSize = 11;
        perfText.color = new Color(0.7f, 0.7f, 0.7f);
        perfText.lineSpacing = 18;
    }

    // ─── Status Panel (Top-Right) ─────────────────────────────────────────

    private void CreateStatusPanel(Transform canvasTransform)
    {
        GameObject statusGO = new GameObject("ConnectionStatus");
        statusGO.transform.SetParent(canvasTransform, false);
        RectTransform statusRT = statusGO.AddComponent<RectTransform>();
        statusRT.anchorMin = new Vector2(1, 1);
        statusRT.anchorMax = new Vector2(1, 1);
        statusRT.pivot = new Vector2(1, 1);
        statusRT.anchoredPosition = new Vector2(-10, -10);
        statusRT.sizeDelta = new Vector2(160, 30);

        statusText = MakeTextMeshPro(statusGO, "Disconnected");
        statusText.alignment = TextAlignmentOptions.Right;
        statusText.fontSize = 14;
        statusText.color = Color.gray;
    }

    // ─── Notice Panel (Center) ────────────────────────────────────────────

    private void CreateNoticePanel(Transform canvasTransform)
    {
        GameObject noticeGO = new GameObject("Notice");
        noticeGO.transform.SetParent(canvasTransform, false);
        RectTransform noticeRT = noticeGO.AddComponent<RectTransform>();
        noticeRT.anchorMin = new Vector2(0.5f, 0.8f);
        noticeRT.anchorMax = new Vector2(0.5f, 0.8f);
        noticeRT.pivot = new Vector2(0.5f, 0.5f);
        noticeRT.sizeDelta = new Vector2(600, 60);

        noticeText = MakeTextMeshPro(noticeGO, "");
        noticeText.alignment = TextAlignmentOptions.Center;
        noticeText.fontSize = 24;
        noticeText.color = Color.yellow;
    }

    // ─── Chat Panel (Bottom-Left) ─────────────────────────────────────────

    private void CreateChatPanel(Transform canvasTransform)
    {
        GameObject chatGO = new GameObject("ChatBox");
        chatGO.transform.SetParent(canvasTransform, false);
        RectTransform chatRT = chatGO.AddComponent<RectTransform>();
        chatRT.anchorMin = new Vector2(0, 0);
        chatRT.anchorMax = new Vector2(0.5f, 0);
        chatRT.pivot = new Vector2(0, 0);
        chatRT.anchoredPosition = new Vector2(10, 10);
        chatRT.sizeDelta = new Vector2(500, 200);

        chatText = MakeTextMeshPro(chatGO, "");
        chatText.alignment = TextAlignmentOptions.BottomLeft;
        chatText.fontSize = 14;
        chatText.color = Color.white;
    }

    // ─── UI Helpers ───────────────────────────────────────────────────────

    /// <summary>Helper to create a TextMeshProUGUI on a child GameObject.</summary>
    private static TextMeshProUGUI MakeTextMeshPro(GameObject parent, string initialText)
    {
        GameObject go = new GameObject("Text");
        go.transform.SetParent(parent.transform, false);

        RectTransform rt = go.AddComponent<RectTransform>();
        rt.anchorMin = Vector2.zero;
        rt.anchorMax = Vector2.one;
        rt.offsetMin = Vector2.zero;
        rt.offsetMax = Vector2.zero;

        TextMeshProUGUI text = go.AddComponent<TextMeshProUGUI>();
        text.text = initialText;
        text.fontSize = 16;
        text.color = Color.white;
        text.richText = true;
        return text;
    }

    // ─── Connection Status Updates ──────────────────────────────────────

    /// <summary>
    /// Update the connection status indicator (top-right corner).
    /// Colors: green=connected, gray=disconnected, yellow=reconnecting.
    /// </summary>
    public void UpdateConnectionStatus(string status, Color color)
    {
        if (statusText == null) return;
        if (status == lastStatus) return;
        lastStatus = status;
        statusText.text = status;
        statusText.color = color;
    }

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>
    /// Update the HUD with the local player's latest stats.
    /// Co giãn thanh HP/MP dựa trên tỷ lệ phần trăm.
    /// Chuyển màu thanh HP từ xanh sang đỏ khi máu thấp (< 30%).
    /// </summary>
    public void UpdateHUD(Decoders.StatsPacket stats)
    {
        // ── HP Bar ──
        if (hpBarFill != null)
        {
            float hpPct = stats.MaxHP > 0 ? Mathf.Clamp01((float)stats.HP / stats.MaxHP) : 0f;
            // Update fill amount (Filled image horizontally)
            Image fillImg = hpBarFill.GetComponent<Image>();
            if (fillImg != null)
            {
                fillImg.fillAmount = hpPct;
                // Transition color: green -> yellow -> red as HP decreases
                if (hpPct < 0.3f)
                    fillImg.color = HpBarDamageColor;
                else if (hpPct < 0.6f)
                    fillImg.color = Color.Lerp(HpBarDamageColor, HpBarColor, (hpPct - 0.3f) / 0.3f);
                else
                    fillImg.color = HpBarColor;
            }
        }

        // ── HP Text ──
        if (hpText != null)
            hpText.text = $"HP: {stats.HP} / {stats.MaxHP}";

        // ── MP Bar ──
        if (mpBarFill != null)
        {
            float mpPct = stats.MaxMP > 0 ? Mathf.Clamp01((float)stats.MP / stats.MaxMP) : 0f;
            Image fillImg = mpBarFill.GetComponent<Image>();
            if (fillImg != null)
                fillImg.fillAmount = mpPct;
        }

        // ── MP Text ──
        if (mpText != null)
            mpText.text = $"MP: {stats.MP} / {stats.MaxMP}";

        // ── Level ──
        if (levelText != null)
            levelText.text = $"Lv: {stats.Level}  ATK: {stats.Dam}";
    }

    /// <summary>Show a damage number floating above the target (placeholder — simple text pop).</summary>
    public void ShowDamageNumber(Decoders.CombatHitPacket packet)
    {
        AppendLine($"<color=red>-{packet.Damage} HP</color> to Entity[{packet.TargetID}]");
    }

    /// <summary>Append a chat message to the chat box.</summary>
    public void AppendChat(Decoders.ChatPacket packet)
    {
        string channelTag = packet.Channel == 1 ? "[Party]" : "";
        AppendLine($"{channelTag}<b>{packet.SenderName}</b>: {packet.Message}");
    }

    /// <summary>Show a server notice (yellow, fades after a few seconds).</summary>
    public void ShowNotice(Decoders.NoticePacket packet)
    {
        noticeText.text = packet.Message;
        CancelInvoke(nameof(ClearNotice));
        Invoke(nameof(ClearNotice), 5f);
    }

    /// <summary>Show a system error (Notice + Red chat message).</summary>
    public void ShowError(Decoders.ErrorPacket packet)
    {
        noticeText.text = $"<color=red>Error {packet.ErrorCode}:</color> {packet.Message}";
        CancelInvoke(nameof(ClearNotice));
        Invoke(nameof(ClearNotice), 6f);
        AppendLine($"<color=red>[SYSTEM ERROR {packet.ErrorCode}] {packet.Message}</color>");
    }

    private void ClearNotice()
    {
        noticeText.text = "";
    }

    /// <summary>Append a formatted line to the chat buffer.</summary>
    private void AppendLine(string line)
    {
        chatLines.Add(line);
        while (chatLines.Count > MaxChatLines)
            chatLines.RemoveAt(0);

        chatText.text = string.Join("\n", chatLines);
    }
}