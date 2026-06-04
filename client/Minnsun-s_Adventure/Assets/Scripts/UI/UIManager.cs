using UnityEngine;
using UnityEngine.UI;

/// <summary>
/// Manages all in-game UI: HUD, chat box, combat damage numbers, notices.
/// All UI elements are created in code using uGUI (no Canvas prefabs).
/// </summary>
public class UIManager : MonoBehaviour
{
    // ─── HUD Elements ──────────────────────────────────────────────
    private Text hudText;
    private Text noticeText;
    private Text chatText;

    // ─── Chat buffer ───────────────────────────────────────────────
    private readonly System.Collections.Generic.List<string> chatLines = new System.Collections.Generic.List<string>();
    private const int MaxChatLines = 50;

    private void Awake()
    {
        CreateCanvas();
    }

    /// <summary>
    /// Create the entire UI hierarchy in code — no prefabs needed.
    /// </summary>
    private void CreateCanvas()
    {
        // Root Canvas
        GameObject canvasGO = new GameObject("UI_Canvas");
        Canvas canvas = canvasGO.AddComponent<Canvas>();
        canvas.renderMode = RenderMode.ScreenSpaceOverlay;
        canvasGO.AddComponent<CanvasScaler>();
        canvasGO.AddComponent<GraphicRaycaster>();

        // ── HUD (top-left) ──
        GameObject hudGO = new GameObject("HUD");
        hudGO.transform.SetParent(canvasGO.transform, false);
        RectTransform hudRT = hudGO.AddComponent<RectTransform>();
        hudRT.anchorMin = new Vector2(0, 1);
        hudRT.anchorMax = new Vector2(0, 1);
        hudRT.pivot = new Vector2(0, 1);
        hudRT.anchoredPosition = new Vector2(10, -10);
        hudRT.sizeDelta = new Vector2(300, 120);

        hudText = MakeText(hudGO, "HP: - / -\nMP: - / -\nLv: -  ATK: -");

        // ── Notice (center) ──
        GameObject noticeGO = new GameObject("Notice");
        noticeGO.transform.SetParent(canvasGO.transform, false);
        RectTransform noticeRT = noticeGO.AddComponent<RectTransform>();
        noticeRT.anchorMin = new Vector2(0.5f, 0.8f);
        noticeRT.anchorMax = new Vector2(0.5f, 0.8f);
        noticeRT.pivot = new Vector2(0.5f, 0.5f);
        noticeRT.sizeDelta = new Vector2(600, 60);

        noticeText = MakeText(noticeGO, "");
        noticeText.alignment = TextAnchor.MiddleCenter;
        noticeText.fontSize = 24;
        noticeText.color = Color.yellow;

        // ── Chat (bottom-left) ──
        GameObject chatGO = new GameObject("ChatBox");
        chatGO.transform.SetParent(canvasGO.transform, false);
        RectTransform chatRT = chatGO.AddComponent<RectTransform>();
        chatRT.anchorMin = new Vector2(0, 0);
        chatRT.anchorMax = new Vector2(0.5f, 0);
        chatRT.pivot = new Vector2(0, 0);
        chatRT.anchoredPosition = new Vector2(10, 10);
        chatRT.sizeDelta = new Vector2(500, 200);

        chatText = MakeText(chatGO, "");
        chatText.alignment = TextAnchor.LowerLeft;
        chatText.fontSize = 14;
        chatText.color = Color.white;

        // ── Damage Number parent ──
        GameObject dmgRoot = new GameObject("DamageRoot");
        dmgRoot.transform.SetParent(canvasGO.transform, false);
    }

    /// <summary>Helper to create a Text on a child GameObject.</summary>
    private static Text MakeText(GameObject parent, string initialText)
    {
        GameObject go = new GameObject("Text");
        go.transform.SetParent(parent.transform, false);

        RectTransform rt = go.AddComponent<RectTransform>();
        rt.anchorMin = Vector2.zero;
        rt.anchorMax = Vector2.one;
        rt.offsetMin = Vector2.zero;
        rt.offsetMax = Vector2.zero;

        Text text = go.AddComponent<Text>();
        text.text = initialText;
        text.font = Resources.GetBuiltinResource<Font>("Arial.ttf");
        text.fontSize = 16;
        text.color = Color.white;
        text.supportRichText = true;
        return text;
    }

    // ─── Public API ────────────────────────────────────────────────

    /// <summary>Update the HUD with the local player's latest stats.</summary>
    public void UpdateHUD(Decoders.StatsPacket stats)
    {
        hudText.text = $"HP: {stats.HP} / {stats.MaxHP}\n" +
                       $"MP: {stats.MP} / {stats.MaxMP}\n" +
                       $"Lv: {stats.Level}  ATK: {stats.Dam}";
    }

    /// <summary>Show a damage number floating above the target (placeholder — simple text pop).</summary>
    public void ShowDamageNumber(Decoders.CombatHitPacket packet)
    {
        // For now, just log to chat-style line as a placeholder
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