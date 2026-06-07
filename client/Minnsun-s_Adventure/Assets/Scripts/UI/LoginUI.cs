using UnityEngine;

/// <summary>
/// A simple IMGUI based Login and Register screen.
/// </summary>
public class LoginUI : MonoBehaviour
{
    private string username = "test";
    private string password = "password123";
    private bool showUI = true;

    private void Start()
    {
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Subscribe(OnSpawned);
    }

    private void OnDestroy()
    {
        EventBus<EventBusDispatcher.EntitySpawnedEvent>.Unsubscribe(OnSpawned);
    }

    private void OnSpawned(EventBusDispatcher.EntitySpawnedEvent evt)
    {
        var reg = ServiceContainer.Resolve<EntityRegistry>();
        if (reg != null && reg.LocalPlayerID == evt.EntityID)
        {
            showUI = false;
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
                Debug.LogError("Failed to reconnect to server.");
                yield break;
            }
        }
        
        net.SendPacket(opcode, payload);
    }

    private void OnGUI()
    {
        if (!showUI) return;

        int width = 300;
        int height = 200;
        int x = (Screen.width - width) / 2;
        int y = (Screen.height - height) / 2;

        GUI.Box(new Rect(x, y, width, height), "Login / Register");

        GUI.Label(new Rect(x + 20, y + 40, 80, 25), "Username:");
        username = GUI.TextField(new Rect(x + 100, y + 40, 160, 25), username);

        GUI.Label(new Rect(x + 20, y + 80, 80, 25), "Password:");
        password = GUI.PasswordField(new Rect(x + 100, y + 80, 160, 25), password, '*');

        if (GUI.Button(new Rect(x + 40, y + 130, 100, 30), "Login"))
        {
            SendAuthPacket(Opcodes.C2SLogin, PacketWriter.WriteLogin(username, password));
        }

        if (GUI.Button(new Rect(x + 160, y + 130, 100, 30), "Register"))
        {
            SendAuthPacket(Opcodes.C2SRegister, PacketWriter.WriteRegister(username, password));
        }
    }
}
