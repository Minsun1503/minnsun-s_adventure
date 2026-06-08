using System;
using System.Collections.Concurrent;
using UnityEngine;

/// <summary>
/// Singleton MonoBehaviour that dispatches actions from background threads
/// onto the Unity main thread via a ConcurrentQueue drained in Update().
/// Usage: MainThreadDispatcher.Instance.Enqueue(() => { /* Unity API calls */ });
/// </summary>
public class MainThreadDispatcher : MonoBehaviour
{
    private static MainThreadDispatcher _instance;
    private readonly ConcurrentQueue<Action> _queue = new ConcurrentQueue<Action>();

    public static MainThreadDispatcher Instance
    {
        get
        {
            if (_instance == null)
            {
                GameObject go = new GameObject("MainThreadDispatcher");
                DontDestroyOnLoad(go);
                _instance = go.AddComponent<MainThreadDispatcher>();
            }
            return _instance;
        }
    }

    private void Awake()
    {
        if (_instance != null && _instance != this)
        {
            Destroy(gameObject);
            return;
        }
        _instance = this;
        DontDestroyOnLoad(gameObject);
    }

    private void Update()
    {
        // Drain the queue — execute up to 100 actions per frame to prevent frame drops
        int processed = 0;
        while (processed < 100 && _queue.TryDequeue(out Action action))
        {
            try
            {
                action();
            }
            catch (Exception ex)
            {
                Debug.LogError($"[MainThreadDispatcher] Error executing action: {ex.Message}\n{ex.StackTrace}");
            }
            processed++;
        }
    }

    /// <summary>
    /// Enqueue an action to be executed on the Unity main thread.
    /// Thread-safe — can be called from any background thread.
    /// </summary>
    public void Enqueue(Action action)
    {
        if (action == null) return;
        _queue.Enqueue(action);
    }

    private void OnDestroy()
    {
        _instance = null;
    }
}