using UnityEngine;

/// <summary>
/// Pure C# service that acts as a central interface for camera configuration.
/// Registered as a singleton in ServiceContainer.
/// Provides high-level API: SetTarget, SetZoom, SetRotation.
/// Underlying implementation delegates to the active FollowCamera component.
/// </summary>
public class CameraService
{
    private FollowCamera followCamera;

    /// <summary>
    /// Initialize with a reference to the FollowCamera component.
    /// Called during bootstrap after FollowCamera is attached.
    /// </summary>
    public void Init(FollowCamera cam)
    {
        followCamera = cam;
    }

    /// <summary>
    /// Set the target transform for the camera to follow.
    /// </summary>
    public void SetTarget(Transform target)
    {
        if (followCamera != null)
            followCamera.SetTarget(target);
        else
            Logger.W("CameraService", "SetTarget called but FollowCamera is not initialized");
    }

    /// <summary>
    /// Set zoom level as a multiplier relative to the default orthographic size.
    /// Clamped internally by FollowCamera's min/max zoom bounds.
    /// </summary>
    public void SetZoom(float factor)
    {
        if (followCamera != null)
            followCamera.SetZoom(factor);
        else
            Logger.W("CameraService", "SetZoom called but FollowCamera is not initialized");
    }

    /// <summary>
    /// Set the absolute yaw (horizontal rotation) angle in degrees.
    /// </summary>
    public void SetRotation(float yawAngle)
    {
        if (followCamera != null)
            followCamera.SetRotation(yawAngle);
        else
            Logger.W("CameraService", "SetRotation called but FollowCamera is not initialized");
    }
}