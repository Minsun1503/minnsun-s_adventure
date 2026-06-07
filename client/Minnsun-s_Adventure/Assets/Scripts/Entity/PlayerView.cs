using UnityEngine;

/// <summary>
/// Player entity view — blue Capsule.
/// If this is the local player, automatically attaches PlayerController
/// and sets camera via CameraService to follow this target.
/// </summary>
public class PlayerView : EntityView
{
    /// <summary>Whether this view represents the local player.</summary>
    public bool IsLocalPlayer { get; private set; }

    /// <summary>
    /// Initialize this player view.
    /// If entityID matches registry.LocalPlayerID, attach PlayerController + camera follow.
    /// </summary>
    public void InitAsLocalPlayer(ulong localPlayerID)
    {
        IsLocalPlayer = (EntityID == localPlayerID);

        if (!IsLocalPlayer)
            return;

        // Attach PlayerController for WASD movement + attack input
        gameObject.AddComponent<PlayerController>();

        // Set camera to follow this player via CameraService
        CameraService cameraService = ServiceContainer.Resolve<CameraService>();
        if (cameraService != null)
        {
            cameraService.SetTarget(transform);
            Logger.D("PlayerView", "CameraService.SetTarget called for local player {0} (ID={1})", EntityName, EntityID);
        }
        else
        {
            Logger.W("PlayerView", "CameraService not registered — camera will not follow player");
        }

        Logger.I("PlayerView", "Local player initialized: {0} (ID={1})", EntityName, EntityID);
    }

    protected override void Update()
    {
        // Local player tự quản lý vị trí qua PlayerController — không lerp.
        // Skip EntityView.Update() để tránh xung đột giữa 2 hệ thống
        // (PlayerController đẩy transform.position, EntityView kéo về TargetPosition cũ).
        if (IsLocalPlayer)
            return;

        // Remote players vẫn lerp về target position như bình thường
        base.Update();
    }
}
