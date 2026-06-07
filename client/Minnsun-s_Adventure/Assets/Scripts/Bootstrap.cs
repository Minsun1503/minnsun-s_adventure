using UnityEngine;

/// <summary>
/// Legacy Bootstrap — now redirects to GameBootstrap.
/// This file exists only so existing scenes with a Bootstrap component
/// continue to work. Remove once all scenes reference GameBootstrap directly.
/// </summary>
public class Bootstrap : MonoBehaviour
{
    private GameBootstrap gameBootstrap;

    private void Awake()
    {
        // Destroy self and let GameBootstrap handle initialization
        // If GameBootstrap already exists, just destroy this
        var existing = FindObjectOfType<GameBootstrap>();
        if (existing != null)
        {
            Debug.Log("[Bootstrap] GameBootstrap already exists — destroying legacy Bootstrap");
            Destroy(gameObject);
            return;
        }

        // GameBootstrap not found — add it and destroy self
        Debug.Log("[Bootstrap] Adding GameBootstrap and destroying legacy Bootstrap");
        gameObject.AddComponent<GameBootstrap>();
        Destroy(this);
    }
}