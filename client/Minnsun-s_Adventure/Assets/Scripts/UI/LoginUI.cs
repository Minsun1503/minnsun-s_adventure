using UnityEngine;
using UnityEngine.UI;
using TMPro;

/// <summary>
/// Canvas-based Login and Register screen.
/// Created programmatically — no prefabs needed.
/// UIBridge can scan/click/type elements by name (input_username, input_password, btn_login, btn_register).
/// </summary>
public class LoginUI : MonoBehaviour
{
    private string username = "test";
    private string password = "password123";

    // Canvas references for UIBridge scanning
    private GameObject canvasGO;
    private InputField inputUsername;
    private InputField inputPassword;

    private void Start()
    {
        CreateLoginCanvas();
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Subscribe(OnSpawned);
    }

    private void OnDestroy()
    {
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Unsubscribe(OnSpawned);
        if (canvasGO != null)
            Destroy(canvasGO);
    }

    private void OnSpawned(EventBusDispatcher.EntitySpawnedEvent evt)
    {
        var reg = ServiceContainer.Resolve<EntityRegistry>();
        if (reg != null && reg.LocalPlayerID == evt.EntityID)
        {
            if (canvasGO != null)
                canvasGO.SetActive(false);
        }
    }

    private void SendAuthPacket(byte opcode, byte[] payload)
    {
        StartCoroutine(ConnectAndSend(opcode, payload));
    }

    private System.Collections.IEnumerator ConnectAndSend(byte opcode, byte[] payload)
    {
        var net = ServiceContainer.Resolve<NetworkManager>();
        if (net == null) yield break;

        if (!net.IsConnected)
        {
            net.Reconnect();
            float t = 0;
            while (!net.IsConnected && t < 3f)
            {
                t += Time.deltaTime;
                yield return null;
            }
            if (!net.IsConnected)
            {
                Debug.LogError("[LoginUI] Failed to reconnect to server.");
                yield break;
            }
        }

        net.SendPacket(opcode, payload);
    }

    // ─── Canvas UI Creation ──────────────────────────────────────────────

    private void CreateLoginCanvas()
    {
        canvasGO = new GameObject("LoginCanvas");
        Canvas canvas = canvasGO.AddComponent<Canvas>();
        canvas.renderMode = RenderMode.ScreenSpaceOverlay;
        canvasGO.AddComponent<CanvasScaler>();
        canvasGO.AddComponent<GraphicRaycaster>();

        // ── Background panel ──
        GameObject bgPanel = new GameObject("LoginPanel");
        bgPanel.transform.SetParent(canvasGO.transform, false);
        RectTransform bgRT = bgPanel.AddComponent<RectTransform>();
        bgRT.anchorMin = new Vector2(0.5f, 0.5f);
        bgRT.anchorMax = new Vector2(0.5f, 0.5f);
        bgRT.pivot = new Vector2(0.5f, 0.5f);
        bgRT.sizeDelta = new Vector2(320, 260);
        bgRT.anchoredPosition = Vector2.zero;

        Image bgImg = bgPanel.AddComponent<Image>();
        bgImg.color = new Color(0.2f, 0.2f, 0.2f, 0.9f);

        // ── Title ──
        CreateLabel(bgPanel.transform, "Title", "Login / Register",
            new Vector2(0, 100), new Vector2(280, 30), 18, Color.white);

        // ── Username Label ──
        CreateLabel(bgPanel.transform, "Label_username", "Username:",
            new Vector2(0, 60), new Vector2(80, 25), 14, Color.white);

        // ── Username Input ──
        inputUsername = CreateInputField(bgPanel.transform, "input_username",
            new Vector2(0, 60), new Vector2(190, 28), 14);

        // ── Password Label ──
        CreateLabel(bgPanel.transform, "Label_password", "Password:",
            new Vector2(0, 20), new Vector2(80, 25), 14, Color.white);

        // ── Password Input ──
        inputPassword = CreateInputField(bgPanel.transform, "input_password",
            new Vector2(0, 20), new Vector2(190, 28), 14);
        inputPassword.contentType = InputField.ContentType.Password;

        // Set initial values
        inputUsername.text = username;
        inputPassword.text = password;

        // Add listeners to sync back to fields
        inputUsername.onValueChanged.AddListener((val) => { username = val; });
        inputPassword.onValueChanged.AddListener((val) => { password = val; });

        // ── Login Button ──
        CreateButton(bgPanel.transform, "btn_login", "Login",
            new Vector2(-75, -40), new Vector2(120, 35), () =>
            {
                SendAuthPacket(Opcodes.C2SLogin, PacketWriter.WriteLogin(username, password));
            });

        // ── Register Button ──
        CreateButton(bgPanel.transform, "btn_register", "Register",
            new Vector2(75, -40), new Vector2(120, 35), () =>
            {
                SendAuthPacket(Opcodes.C2SRegister, PacketWriter.WriteRegister(username, password));
            });
    }

    // ─── UI Helpers ──────────────────────────────────────────────────────

    private void CreateLabel(Transform parent, string name, string text,
        Vector2 anchoredPos, Vector2 sizeDelta, int fontSize, Color color)
    {
        GameObject go = new GameObject(name);
        go.transform.SetParent(parent, false);

        RectTransform rt = go.AddComponent<RectTransform>();
        rt.anchorMin = new Vector2(0.5f, 0.5f);
        rt.anchorMax = new Vector2(0.5f, 0.5f);
        rt.pivot = new Vector2(0.5f, 0.5f);
        rt.anchoredPosition = anchoredPos;
        rt.sizeDelta = sizeDelta;

        Text txt = go.AddComponent<Text>();
        txt.text = text;
        txt.fontSize = fontSize;
        txt.color = color;
        txt.alignment = TextAnchor.MiddleLeft;
        txt.font = Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
    }

    private InputField CreateInputField(Transform parent, string name,
        Vector2 anchoredPos, Vector2 sizeDelta, int fontSize)
    {
        // Background (InputField requires a target Graphic)
        GameObject bg = new GameObject(name);
        bg.transform.SetParent(parent, false);

        RectTransform bgRT = bg.AddComponent<RectTransform>();
        bgRT.anchorMin = new Vector2(0.5f, 0.5f);
        bgRT.anchorMax = new Vector2(0.5f, 0.5f);
        bgRT.pivot = new Vector2(0.5f, 0.5f);
        bgRT.anchoredPosition = anchoredPos;
        bgRT.sizeDelta = sizeDelta;

        Image bgImg = bg.AddComponent<Image>();
        bgImg.color = Color.white;

        // Text child
        GameObject textGO = new GameObject("Text");
        textGO.transform.SetParent(bg.transform, false);

        RectTransform textRT = textGO.AddComponent<RectTransform>();
        textRT.anchorMin = Vector2.zero;
        textRT.anchorMax = Vector2.one;
        textRT.offsetMin = new Vector2(6, 2);
        textRT.offsetMax = new Vector2(-6, -2);

        Text txt = textGO.AddComponent<Text>();
        txt.fontSize = fontSize;
        txt.color = Color.black;
        txt.alignment = TextAnchor.MiddleLeft;
        txt.font = Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");

        // InputField component
        InputField inputField = bg.AddComponent<InputField>();
        inputField.textComponent = txt;
        inputField.targetGraphic = bgImg;

        return inputField;
    }

    private void CreateButton(Transform parent, string name, string buttonText,
        Vector2 anchoredPos, Vector2 sizeDelta, UnityEngine.Events.UnityAction onClick)
    {
        GameObject go = new GameObject(name);
        go.transform.SetParent(parent, false);

        RectTransform rt = go.AddComponent<RectTransform>();
        rt.anchorMin = new Vector2(0.5f, 0.5f);
        rt.anchorMax = new Vector2(0.5f, 0.5f);
        rt.pivot = new Vector2(0.5f, 0.5f);
        rt.anchoredPosition = anchoredPos;
        rt.sizeDelta = sizeDelta;

        Image img = go.AddComponent<Image>();
        img.color = new Color(0.3f, 0.5f, 0.8f);

        Button btn = go.AddComponent<Button>();
        btn.targetGraphic = img;
        btn.onClick.AddListener(onClick);

        // Button text child
        GameObject label = new GameObject("Label");
        label.transform.SetParent(go.transform, false);

        RectTransform labelRT = label.AddComponent<RectTransform>();
        labelRT.anchorMin = Vector2.zero;
        labelRT.anchorMax = Vector2.one;
        labelRT.offsetMin = Vector2.zero;
        labelRT.offsetMax = Vector2.zero;

        Text btnText = label.AddComponent<Text>();
        btnText.text = buttonText;
        btnText.fontSize = 14;
        btnText.color = Color.white;
        btnText.alignment = TextAnchor.MiddleCenter;
        btnText.font = Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
    }
}