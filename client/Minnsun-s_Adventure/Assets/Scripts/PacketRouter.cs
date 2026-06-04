using UnityEngine;

/// <summary>
/// Central packet router — dispatches all server-to-client packets to the correct handler.
/// Both NetworkClient and NetworkClientWS call this.Route() after receiving a binary frame.
/// </summary>
public class PacketRouter : MonoBehaviour
{
    private EntityManager entityManager;
    private UIManager uiManager;

    /// <summary>
    /// Called by Bootstrap after creating all components.
    /// This avoids FindObjectOfType scans while correctly handling
    /// the fact that UIManager sits on a separate UIRoot GameObject.
    /// </summary>
    public void Init(EntityManager em, UIManager ui)
    {
        entityManager = em;
        uiManager = ui;
    }

    /// <summary>
    /// Route an incoming packet by opcode.
    /// Called from NetworkClient/NetworkClientWS on the Unity main thread.
    /// </summary>
    /// <param name="opcode">Server-to-client opcode byte.</param>
    /// <param name="data">Payload bytes (everything after the opcode byte).</param>
    public void Route(byte opcode, byte[] data)
    {
        switch (opcode)
        {
            case Opcodes.S2CSuccess:
            {
                var packet = Decoders.DecodeSuccess(data);
                if (packet.HasValue && entityManager != null)
                    entityManager.SetLocalPlayerID(packet.Value.EntityID);
                break;
            }

            case Opcodes.S2CSpawnEntity:
            {
                var packet = Decoders.DecodeSpawn(data);
                if (packet.HasValue && entityManager != null)
                    entityManager.Spawn(packet.Value);
                break;
            }

            case Opcodes.S2CDespawnEntity:
            {
                var packet = Decoders.DecodeDespawn(data);
                if (packet.HasValue && entityManager != null)
                    entityManager.Despawn(packet.Value.EntityID);
                break;
            }

            case Opcodes.S2CPositionSync:
            {
                var packet = Decoders.DecodePosition(data);
                if (packet.HasValue && entityManager != null)
                    entityManager.UpdatePosition(packet.Value);
                break;
            }

            case Opcodes.S2CStatsSync:
            {
                var packet = Decoders.DecodeStats(data);
                if (packet.HasValue && entityManager != null)
                    entityManager.UpdateStats(packet.Value);
                break;
            }

            case Opcodes.S2CCombatHit:
            {
                var packet = Decoders.DecodeCombat(data);
                if (packet.HasValue)
                {
                    if (uiManager != null)
                        uiManager.ShowDamageNumber(packet.Value);
                    if (entityManager != null)
                        entityManager.UpdateHP(packet.Value);
                }
                break;
            }

            case Opcodes.S2CChat:
            {
                var packet = Decoders.DecodeChat(data);
                if (packet.HasValue && uiManager != null)
                    uiManager.AppendChat(packet.Value);
                break;
            }

            case Opcodes.S2CNotice:
            {
                var packet = Decoders.DecodeNotice(data);
                if (packet.HasValue && uiManager != null)
                    uiManager.ShowNotice(packet.Value);
                break;
            }

            case Opcodes.S2CHeartbeat:
                // Already handled silently before reaching Route() — ignore.
                break;

            default:
                Debug.LogWarning($"[Router] Unknown opcode: 0x{opcode:X2}, length={data.Length}");
                break;
        }
    }
}