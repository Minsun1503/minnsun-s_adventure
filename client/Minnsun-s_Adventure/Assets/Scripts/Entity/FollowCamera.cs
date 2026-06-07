using UnityEngine;

/// <summary>
/// Attached to the Main Camera at bootstrap.
/// Provides smooth target following, mouse-wheel zoom, and right-click rotation.
///
/// Zoom:  Mouse scroll wheel adjusts orthographicSize within [minZoom, maxZoom].
/// Rotate: Hold right mouse button and drag horizontally to orbit the camera
///         yaw angle around the target.
///
/// The camera always looks at its target via LookAt().
/// Position is computed as: target.position + yaw-rotated baseOffset.
/// </summary>
public class FollowCamera : MonoBehaviour
{
    [SerializeField] private Transform target;

    // ─── Config ──────────────────────────────────────────────────────────

    /// <summary>Base offset from target when yaw = 0 (set from GameConfig).</summary>
    public Vector3 baseOffset = new Vector3(0, 15, -10);

    /// <summary>Smooth follow speed.</summary>
    public float followSpeed = 5f;

    // ─── Zoom ────────────────────────────────────────────────────────────

    [Header("Zoom")]
    public float minZoom = 5f;
    public float maxZoom = 30f;
    public float zoomSpeed = 3f;
    [SerializeField] private float zoomSmoothSpeed = 8f;

    private float targetOrthoSize;

    // ─── Rotation ────────────────────────────────────────────────────────

    [Header("Rotation")]
    public float rotateSpeed = 180f; // degrees per full screen-width drag

    /// <summary>Current yaw (horizontal orbit) angle in degrees. 0 = default orientation.</summary>
    private float yawAngle = 0f;

    // ─── Initialization ──────────────────────────────────────────────────

    private void Start()
    {
        Camera cam = GetComponent<Camera>();
        if (cam != null)
            targetOrthoSize = cam.orthographicSize;
        else
            targetOrthoSize = 20f;

        Logger.D("FollowCamera", "Initialized with baseOffset={0} targetOrthoSize={1}",
            baseOffset, targetOrthoSize);
    }

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>
    /// Set the target transform for the camera to follow.
    /// Snaps position immediately to avoid initial lerp delay.
    /// </summary>
    public void SetTarget(Transform newTarget)
    {
        target = newTarget;
        if (target != null)
        {
            // Snap immediately to avoid initial lerp delay
            Quaternion yawRotation = Quaternion.Euler(0, yawAngle, 0);
            Vector3 rotatedOffset = yawRotation * baseOffset;
            transform.position = target.position + rotatedOffset;
            transform.LookAt(target);
        }
        Logger.D("FollowCamera", "Target set to {0}", newTarget != null ? newTarget.name : "null");
    }

    /// <summary>
    /// Set the target orthographic size (zoom) directly.
    /// Clamped to [minZoom, maxZoom].
    /// </summary>
    public void SetZoom(float size)
    {
        targetOrthoSize = Mathf.Clamp(size, minZoom, maxZoom);
    }

    /// <summary>
    /// Set absolute yaw angle in degrees.
    /// </summary>
    public void SetRotation(float angle)
    {
        yawAngle = angle;
    }

    // ─── Input Handling ──────────────────────────────────────────────────

    private void HandleZoomInput()
    {
        float scroll = Input.GetAxis("Mouse ScrollWheel");
        if (Mathf.Abs(scroll) > 0.001f)
        {
            targetOrthoSize = Mathf.Clamp(
                targetOrthoSize - scroll * zoomSpeed,
                minZoom,
                maxZoom
            );
        }
    }

    private void HandleRotationInput()
    {
        // Right mouse button held + mouse X movement
        if (Input.GetMouseButton(1))
        {
            float mouseX = Input.GetAxis("Mouse X");
            if (Mathf.Abs(mouseX) > 0.001f)
            {
                yawAngle += mouseX * rotateSpeed * Time.deltaTime;
            }
        }
    }

    // ─── Update Loop ─────────────────────────────────────────────────────

    private void Update()
    {
        HandleZoomInput();
        HandleRotationInput();

        // Smoothly interpolate orthographic size
        Camera cam = GetComponent<Camera>();
        if (cam != null)
        {
            cam.orthographicSize = Mathf.Lerp(
                cam.orthographicSize,
                targetOrthoSize,
                zoomSmoothSpeed * Time.deltaTime
            );
        }
    }

    private void LateUpdate()
    {
        if (target == null) return;

        // Rotate the base offset around Y axis by current yaw
        Quaternion yawRotation = Quaternion.Euler(0, yawAngle, 0);
        Vector3 rotatedOffset = yawRotation * baseOffset;

        // Smoothly move toward target + rotated offset
        Vector3 desiredPosition = target.position + rotatedOffset;
        transform.position = Vector3.Lerp(
            transform.position,
            desiredPosition,
            followSpeed * Time.deltaTime
        );

        // Always look at the target
        transform.LookAt(target);
    }
}