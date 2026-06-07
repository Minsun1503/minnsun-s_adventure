using UnityEngine;

/// <summary>
/// Attached to the local player's GameObject.
/// Handles WASD movement (camera-relative, throttled 250ms) and Space attack (throttled 500ms).
/// Left-click selects a monster target via raycast; Space attacks the selected target.
/// Shows a yellow TargetIndicator ring beneath the currently selected target.
/// Uses PacketWriter for building binary payloads.
/// Resolves EntityRegistry from ServiceContainer (no more FindObjectOfType).
/// </summary>
public class PlayerController : MonoBehaviour
{
    private const float MoveSpeed = 8f;
    private const float MoveSendInterval = 0.25f;   // 4 packets/s, matches server tick rate
    private const float AttackCooldown = 0.5f;       // 500ms between attacks
    private const float MaxAttackRange = 15f;         // max distance to attack target

    private NetworkManager networkManager;
    private EntityRegistry entityRegistry;
    private float moveSendTimer;
    private float attackCooldownTimer;
    private Vector3 lastSentPos;

    // ─── Targeting ───────────────────────────────────────────────────────
    private ulong selectedTargetID;                     // 0 = no target selected
    private GameObject indicatorGO;                     // root GameObject for TargetIndicator
    private TargetIndicator targetIndicator;            // procedural ring component

    private void Start()
    {
        networkManager = ServiceContainer.Resolve<NetworkManager>();
        entityRegistry = ServiceContainer.Resolve<EntityRegistry>();

        // Create TargetIndicator as a child GameObject (no prefab needed)
        indicatorGO = new GameObject("TargetIndicator");
        indicatorGO.transform.SetParent(transform, false);
        indicatorGO.SetActive(false); // hidden until a target is selected
        targetIndicator = indicatorGO.AddComponent<TargetIndicator>();

        Logger.D("PlayerController", "Initialized with camera-relative movement + mouse targeting");
    }

    private void Update()
    {
        HandleTargetingInput();      // left-click raycast targeting (runs every frame for click detection)
        HandleMovementInput();       // camera-relative WASD
        HandleAttackInput();         // Space to attack selected target
        UpdateTargetIndicator();     // follow selected target with ring
    }

    // ─── Targeting (Mouse Click) ─────────────────────────────────────────

    /// <summary>
    /// Left-click to select a monster target via Camera raycast.
    /// Clicking empty space deselects.
    /// </summary>
    private void HandleTargetingInput()
    {
        if (!Input.GetMouseButtonDown(0))
            return;

        Ray ray = Camera.main.ScreenPointToRay(Input.mousePosition);
        if (Physics.Raycast(ray, out RaycastHit hit, 200f))
        {
            EntityView hitEntity = hit.collider.GetComponent<EntityView>();
            if (hitEntity != null && hitEntity.EntityType == 1) // Monster only
            {
                selectedTargetID = hitEntity.EntityID;
                Logger.D("PlayerController", "Selected target EntityID={0}", selectedTargetID);
                return;
            }
        }

        // Clicked empty space or non-monster — clear selection
        selectedTargetID = 0;
        Logger.D("PlayerController", "Target deselected (clicked empty space)");
    }

    // ─── Target Indicator ────────────────────────────────────────────────

    /// <summary>
    /// Each frame, move the indicator ring to the selected target's position.
    /// Hide if no target, despawned, or out of range.
    /// </summary>
    private void UpdateTargetIndicator()
    {
        if (selectedTargetID == 0)
        {
            if (indicatorGO.activeSelf)
                indicatorGO.SetActive(false);
            return;
        }

        EntityView targetView = entityRegistry.Get(selectedTargetID);
        if (targetView == null)
        {
            // Target despawned — clear selection
            selectedTargetID = 0;
            indicatorGO.SetActive(false);
            return;
        }

        // Move indicator to target's feet (just above ground)
        Vector3 footPos = targetView.transform.position;
        footPos.y = 0.05f; // just above ground to avoid z-fighting
        indicatorGO.transform.position = footPos;

        if (!indicatorGO.activeSelf)
            indicatorGO.SetActive(true);
    }

    // ─── Movement (Camera-Relative) ──────────────────────────────────────

    private void HandleMovementInput()
    {
        float h = Input.GetAxisRaw("Horizontal");
        float v = Input.GetAxisRaw("Vertical");

        if (h != 0 || v != 0)
        {
            // Camera-relative movement: project camera forward/right onto XZ plane
            Transform camTransform = Camera.main.transform;
            Vector3 camForward = camTransform.forward;
            Vector3 camRight = camTransform.right;
            camForward.y = 0f;
            camRight.y = 0f;
            camForward.Normalize();
            camRight.Normalize();

            Vector3 moveDirection = (camForward * v + camRight * h).normalized;
            transform.position += moveDirection * MoveSpeed * Time.deltaTime;

            // Throttle: only send to server every 250ms or when position changed significantly
            moveSendTimer += Time.deltaTime;
            if (moveSendTimer >= MoveSendInterval && transform.position != lastSentPos)
            {
                SendMovePacket();
                lastSentPos = transform.position;
                moveSendTimer = 0;
            }
        }
    }

    private void SendMovePacket()
    {
        if (networkManager == null) return;

        int x = Mathf.RoundToInt(transform.position.x);
        int z = Mathf.RoundToInt(transform.position.z);
        byte[] payload = PacketWriter.WriteMove(x, z);
        networkManager.SendPacket(Opcodes.C2SMove, payload);
    }

    // ─── Attack ──────────────────────────────────────────────────────────

    private void HandleAttackInput()
    {
        // Cooldown
        attackCooldownTimer += Time.deltaTime;
        if (attackCooldownTimer < AttackCooldown)
            return;

        // Space key only (left-click is now targeting)
        if (Input.GetKeyDown(KeyCode.Space))
        {
            ulong targetID = 0;

            // 1. Try selected target first
            if (selectedTargetID != 0)
            {
                EntityView selectedView = entityRegistry.Get(selectedTargetID);
                if (selectedView != null)
                {
                    float sqrDist = (selectedView.transform.position - transform.position).sqrMagnitude;
                    if (sqrDist <= MaxAttackRange * MaxAttackRange)
                    {
                        targetID = selectedTargetID;
                    }
                    else
                    {
                        Logger.D("PlayerController", "Selected target out of range — falling back to nearest");
                    }
                }
            }

            // 2. Fallback: find nearest target
            if (targetID == 0)
            {
                targetID = FindNearestTarget();
            }

            if (targetID != 0)
            {
                SendAttackPacket(targetID);
                attackCooldownTimer = 0;
            }
        }
    }

    /// <summary>
    /// Find the nearest attackable entity (monster) within MaxAttackRange units.
    /// Uses EntityRegistry to iterate all active entities.
    /// Returns 0 if no valid target found.
    /// </summary>
    private ulong FindNearestTarget()
    {
        if (entityRegistry == null) return 0;

        ulong localID = entityRegistry.LocalPlayerID;
        Vector3 myPos = transform.position;
        float nearestDist = MaxAttackRange * MaxAttackRange; // squared distance
        ulong nearestID = 0;

        foreach (var kvp in entityRegistry.GetAll())
        {
            ulong entityID = kvp.Key;
            EntityView view = kvp.Value;
            if (view == null) continue;
            if (entityID == localID) continue; // skip self

            // Check if monster (EntityType == 1)
            if (view.EntityType == 1)
            {
                float sqrDist = (view.transform.position - myPos).sqrMagnitude;
                if (sqrDist < nearestDist)
                {
                    nearestDist = sqrDist;
                    nearestID = entityID;
                }
            }
        }
        return nearestID;
    }

    private void SendAttackPacket(ulong targetEntityID)
    {
        if (networkManager == null) return;

        byte[] payload = PacketWriter.WriteAttack(targetEntityID);
        networkManager.SendPacket(Opcodes.C2SAttack, payload);
        Debug.Log($"[Player] Attack target={targetEntityID}");
    }
}