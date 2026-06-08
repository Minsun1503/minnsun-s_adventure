using UnityEngine;

/// <summary>
/// Central packet router — dispatches all server-to-client packets via EventBus.
/// Both NetworkClient and NetworkClientWS call this.Route() after receiving a binary frame.
/// Responsibilities:
///   - Decode payload bytes into typed structs (via Decoders)
///   - Publish domain events onto EventBus (EntitySpawned, PlayerMove, StatsUpdated, etc.)
///   - Direct calls to UIManager for UI-only packets (Chat, Notice)
///
/// No direct dependency on EntityManager/EntityService — strictly a decoder + event publisher.
/// </summary>
public class PacketRouter : MonoBehaviour
{
    private UIManager uiManager;
    private Decoders.StatsPacket? pendingStats; // cached if UI not ready yet

    /// <summary>
    /// Called by Bootstrap after creating UI.
    /// Keeps a UIManager reference for UI-only packets (Chat, Notice).
    /// Also flushes any pending stats packet that arrived before Init.
    /// </summary>
    public void Init(UIManager ui)
    {
        uiManager = ui;
        if (pendingStats.HasValue)
        {
            var registry = ServiceContainer.Resolve<EntityRegistry>();
            if (registry != null && pendingStats.Value.EntityID == registry.LocalPlayerID)
                uiManager.UpdateHUD(pendingStats.Value);
            pendingStats = null;
        }
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
                if (packet.HasValue)
                {
                    // Update EntityRegistry LocalPlayerID
                    var registry = ServiceContainer.Resolve<EntityRegistry>();
                    if (registry != null)
                    {
                        registry.LocalPlayerID = packet.Value.EntityID;
                        Logger.D("Router", "LocalPlayerID set to {0}", packet.Value.EntityID);
                    }
                }
                break;
            }

            case Opcodes.S2CSpawnEntity:
            {
                var packet = Decoders.DecodeSpawn(data);
                if (packet.HasValue)
                {
                    EventBus<EventBusDispatcher.EntitySpawnedEvent>.Publish(
                        new EventBusDispatcher.EntitySpawnedEvent
                        {
                            EntityID = packet.Value.EntityID,
                            Type = packet.Value.Type,
                            MapID = packet.Value.MapID,
                            X = packet.Value.X,
                            Z = packet.Value.Z,
                            Name = packet.Value.Name
                        });
                }
                break;
            }

            case Opcodes.S2CDespawnEntity:
            {
                var packet = Decoders.DecodeDespawn(data);
                if (packet.HasValue)
                {
                    EventBus<EventBusDispatcher.EntityDespawnedEvent>.Publish(
                        new EventBusDispatcher.EntityDespawnedEvent
                        {
                            EntityID = packet.Value.EntityID
                        });
                }
                break;
            }

            case Opcodes.S2CPositionSync:
            {
                var packet = Decoders.DecodePosition(data);
                if (packet.HasValue)
                {
                    EventBus<EventBusDispatcher.PlayerMoveEvent>.Publish(
                        new EventBusDispatcher.PlayerMoveEvent
                        {
                            EntityID = packet.Value.EntityID,
                            X = packet.Value.X,
                            Z = packet.Value.Z
                        });
                }
                break;
            }

            case Opcodes.S2CStatsSync:
            {
                var packet = Decoders.DecodeStats(data);
                if (packet.HasValue)
                {
                    // Update stats directly on the EntityView via registry
                    var registry = ServiceContainer.Resolve<EntityRegistry>();
                    if (registry != null)
                    {
                        EntityView view = registry.Get(packet.Value.EntityID);
                        if (view != null)
                        {
                            view.UpdateStats(
                                packet.Value.HP, packet.Value.MaxHP,
                                packet.Value.MP, packet.Value.MaxMP,
                                packet.Value.Dam, packet.Value.Level
                            );

                            // If local player, refresh HUD — cache if UI not ready yet
                            if (packet.Value.EntityID == registry.LocalPlayerID)
                            {
                                if (uiManager != null)
                                    uiManager.UpdateHUD(packet.Value);
                                else
                                    pendingStats = packet.Value; // cache for later
                            }
                        }
                    }

                    // Publish event for other subscribers
                    EventBus<EventBusDispatcher.StatsUpdatedEvent>.Publish(
                        new EventBusDispatcher.StatsUpdatedEvent
                        {
                            EntityID = packet.Value.EntityID
                        });
                }
                break;
            }

            case Opcodes.S2CCombatHit:
            {
                var packet = Decoders.DecodeCombat(data);
                if (packet.HasValue)
                {
                    // Show damage number via UIManager
                    if (uiManager != null)
                        uiManager.ShowDamageNumber(packet.Value);

                    // Fallback: if target is local player, refresh HUD from registry
                    // (in case server StatsSync packet hasn't arrived yet).
                    var registry = ServiceContainer.Resolve<EntityRegistry>();
                    if (registry != null && uiManager != null && packet.Value.TargetID == registry.LocalPlayerID)
                    {
                        EntityView view = registry.Get(packet.Value.TargetID);
                        if (view != null)
                        {
                            var statsPacket = new Decoders.StatsPacket
                            {
                                EntityID = view.EntityID,
                                HP = view.HP,
                                MaxHP = view.MaxHP,
                                MP = view.MP,
                                MaxMP = view.MaxMP,
                                Dam = view.Dam,
                                Level = view.Level
                            };
                            uiManager.UpdateHUD(statsPacket);
                        }
                    }

                    // Publish combat event for EntityService (HP update, flash, despawn)
                    EventBus<EventBusDispatcher.CombatHitEvent>.Publish(
                        new EventBusDispatcher.CombatHitEvent
                        {
                            AttackerID = packet.Value.AttackerID,
                            TargetID = packet.Value.TargetID,
                            Damage = packet.Value.Damage,
                            Killed = packet.Value.Killed
                        });
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

            case Opcodes.S2CError:
            {
                var packet = Decoders.DecodeError(data);
                if (packet.HasValue && uiManager != null)
                    uiManager.ShowError(packet.Value);
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
