using System;
using System.Collections.Generic;
using System.Net;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using TMPro;
using UnityEngine;
using UnityEngine.UI;

/// <summary>
/// TCP-based blackbox testing bridge for Unity.
/// Listens on port 13000, accepts command text lines, returns JSON responses.
///
/// Commands:
///   GET_STATE              — scans active UI elements, returns JSON scene state
///   CLICK|targetName       — invokes Button.onClick on the named GameObject
///   TYPE|targetName|value  — sets InputField.text on the named GameObject
///
/// All Unity API calls are dispatched to the main thread via MainThreadDispatcher.
/// Only active in debug builds (enabled by GameBootstrap when Debug.isDebugBuild).
/// </summary>
public class UIBridge : MonoBehaviour
{
    private const int Port = 13000;
    private TcpListener _listener;
    private Thread _serverThread;
    private volatile bool _running;

    private void Awake()
    {
        _running = true;
        _serverThread = new Thread(ListenerThread)
        {
            IsBackground = true,
            Name = "UIBridge-TCP"
        };
        _serverThread.Start();
        Debug.Log("[UIBridge] Listening on port " + Port);
    }

    private void OnDestroy()
    {
        _running = false;
        try
        {
            _listener?.Stop();
        }
        catch { }

        if (_serverThread != null && _serverThread.IsAlive)
        {
            _serverThread.Join(TimeSpan.FromSeconds(2));
        }
    }

    private void ListenerThread()
    {
        try
        {
            _listener = new TcpListener(IPAddress.Any, Port);
            _listener.Start();
        }
        catch (Exception ex)
        {
            Debug.LogError("[UIBridge] Failed to start TCP listener: " + ex.Message);
            return;
        }

        while (_running)
        {
            try
            {
                using (TcpClient client = _listener.AcceptTcpClient())
                using (NetworkStream stream = client.GetStream())
                {
                    // Read command until newline
                    byte[] buffer = new byte[4096];
                    int bytesRead = stream.Read(buffer, 0, buffer.Length);
                    if (bytesRead <= 0) continue;

                    string rawCommand = Encoding.UTF8.GetString(buffer, 0, bytesRead).Trim();
                    string response = ProcessCommand(rawCommand);

                    byte[] responseBytes = Encoding.UTF8.GetBytes(response + "\n");
                    stream.Write(responseBytes, 0, responseBytes.Length);
                }
            }
            catch (ObjectDisposedException)
            {
                break;
            }
            catch (Exception ex)
            {
                if (_running)
                {
                    Debug.LogError("[UIBridge] TCP error: " + ex.Message);
                }
            }
        }
    }

    /// <summary>
    /// Parse and process a command string. Returns JSON response.
    /// Thread-safe: dispatches Unity API calls to main thread.
    /// </summary>
    private string ProcessCommand(string command)
    {
        if (string.IsNullOrEmpty(command))
            return JsonUtility.ToJson(new { error = "empty command" });

        if (command == "GET_STATE")
        {
            return ProcessGetState();
        }

        string[] parts = command.Split('|');
        if (parts.Length < 2)
            return JsonUtility.ToJson(new { error = "invalid command format: " + command });

        string cmd = parts[0];
        string target = parts[1];

        switch (cmd)
        {
            case "CLICK":
                return ProcessClick(target);
            case "TYPE":
                string value = parts.Length >= 3 ? parts[2] : "";
                return ProcessType(target, value);
            default:
                return JsonUtility.ToJson(new { error = "unknown command: " + cmd });
        }
    }

    // ─── GET_STATE ─────────────────────────────────────────────────────────

    [Serializable]
    private class UIElementData
    {
        public string id;
        public string type;
        public string text;
        public bool interactable;
        public string value;
    }

    [Serializable]
    private class SceneState
    {
        public string scene;
        public List<UIElementData> elements;
    }

    /// <summary>
    /// Scan all active GameObjects in the scene, collect Button/InputField/Text/TMP_Text components.
    /// </summary>
    private string ProcessGetState()
    {
        SceneState state = new SceneState();
        state.elements = new List<UIElementData>();

        // Use ManualResetEvent to synchronize main thread dispatch
        var evt = new ManualResetEvent(false);
        Exception capturedException = null;

        MainThreadDispatcher.Instance.Enqueue(() =>
        {
            try
            {
                // Get current scene name
                state.scene = UnityEngine.SceneManagement.SceneManager.GetActiveScene().name;

                // Find all active GameObjects
                GameObject[] allObjects = FindObjectsByType<GameObject>(FindObjectsSortMode.None);
                foreach (GameObject go in allObjects)
                {
                    if (!go.activeInHierarchy) continue;

                    // Scan for Button
                    Button btn = go.GetComponent<Button>();
                    if (btn != null && btn.enabled)
                    {
                        Text txt = go.GetComponentInChildren<Text>(false);
                        TMP_Text tmp = go.GetComponentInChildren<TMP_Text>(false);
                        string btnText = "";
                        if (tmp != null) btnText = tmp.text;
                        else if (txt != null) btnText = txt.text;

                        state.elements.Add(new UIElementData
                        {
                            id = go.name,
                            type = "Button",
                            text = btnText,
                            interactable = btn.interactable
                        });
                    }

                    // Scan for InputField
                    InputField inputField = go.GetComponent<InputField>();
                    if (inputField != null && inputField.enabled)
                    {
                        state.elements.Add(new UIElementData
                        {
                            id = go.name,
                            type = "InputField",
                            value = inputField.text,
                            interactable = inputField.interactable
                        });
                    }

                    // Scan for TMP_InputField
                    TMP_InputField tmpInput = go.GetComponent<TMP_InputField>();
                    if (tmpInput != null && tmpInput.enabled)
                    {
                        state.elements.Add(new UIElementData
                        {
                            id = go.name,
                            type = "InputField",
                            value = tmpInput.text,
                            interactable = tmpInput.interactable
                        });
                    }

                    // Scan for Text (Legacy)
                    Text legacyText = go.GetComponent<Text>();
                    if (legacyText != null && legacyText.enabled)
                    {
                        // Skip if this is a child of a Button (already captured via GetComponentInChildren)
                        state.elements.Add(new UIElementData
                        {
                            id = go.name,
                            type = "Text",
                            text = legacyText.text,
                            interactable = true
                        });
                    }

                    // Scan for TMP_Text (standalone, not child of Button already captured)
                    TMP_Text tmpText = go.GetComponent<TMP_Text>();
                    if (tmpText != null && tmpText.enabled)
                    {
                        // Check if this is a child of a Button (already captured)
                        if (go.GetComponentInParent<Button>() == null)
                        {
                            state.elements.Add(new UIElementData
                            {
                                id = go.name,
                                type = "Text",
                                text = tmpText.text,
                                interactable = true
                            });
                        }
                    }
                }
            }
            catch (Exception ex)
            {
                capturedException = ex;
            }
            finally
            {
                evt.Set();
            }
        });

        evt.WaitOne(5000); // 5 second timeout

        if (capturedException != null)
        {
            return JsonUtility.ToJson(new { error = "scan failed: " + capturedException.Message });
        }

        if (state.elements == null)
            state.elements = new List<UIElementData>();

        return JsonUtility.ToJson(state);
    }

    // ─── CLICK ────────────────────────────────────────────────────────────

    private string ProcessClick(string targetName)
    {
        var evt = new ManualResetEvent(false);
        string result = "ok";
        Exception capturedException = null;

        MainThreadDispatcher.Instance.Enqueue(() =>
        {
            try
            {
                GameObject go = GameObject.Find(targetName);
                if (go == null)
                {
                    // Try FindObjectsByType as fallback
                    Button[] allButtons = FindObjectsByType<Button>(FindObjectsSortMode.None);
                    foreach (Button b in allButtons)
                    {
                        if (b.name == targetName && b.enabled)
                        {
                            b.onClick.Invoke();
                            result = "clicked";
                            evt.Set();
                            return;
                        }
                    }
                    result = "error: target not found: " + targetName;
                    evt.Set();
                    return;
                }

                Button btn = go.GetComponent<Button>();
                if (btn == null || !btn.enabled)
                {
                    result = "error: target '" + targetName + "' has no enabled Button component";
                    evt.Set();
                    return;
                }

                btn.onClick.Invoke();
                result = "clicked";
            }
            catch (Exception ex)
            {
                capturedException = ex;
                result = "error: " + ex.Message;
            }
            finally
            {
                evt.Set();
            }
        });

        evt.WaitOne(5000);
        return JsonUtility.ToJson(new { status = result });
    }

    // ─── TYPE ─────────────────────────────────────────────────────────────

    private string ProcessType(string targetName, string value)
    {
        var evt = new ManualResetEvent(false);
        string result = "ok";
        Exception capturedException = null;

        MainThreadDispatcher.Instance.Enqueue(() =>
        {
            try
            {
                GameObject go = GameObject.Find(targetName);
                if (go == null)
                {
                    result = "error: target not found: " + targetName;
                    evt.Set();
                    return;
                }

                // Try InputField first, then TMP_InputField
                InputField inputField = go.GetComponent<InputField>();
                if (inputField != null && inputField.enabled)
                {
                    inputField.text = value;
                    result = "typed";
                    evt.Set();
                    return;
                }

                TMP_InputField tmpInput = go.GetComponent<TMP_InputField>();
                if (tmpInput != null && tmpInput.enabled)
                {
                    tmpInput.text = value;
                    result = "typed";
                    evt.Set();
                    return;
                }

                result = "error: target '" + targetName + "' has no enabled InputField/TMP_InputField component";
            }
            catch (Exception ex)
            {
                capturedException = ex;
                result = "error: " + ex.Message;
            }
            finally
            {
                evt.Set();
            }
        });

        evt.WaitOne(5000);
        return JsonUtility.ToJson(new { status = result });
    }
}