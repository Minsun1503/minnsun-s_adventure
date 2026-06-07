using System;
using System.Collections.Generic;
using UnityEngine;

/// <summary>
/// Lightweight service locator — registers and resolves singleton services.
/// Thread-safe for registration; resolve is lock-free after registration phase.
/// </summary>
public static class ServiceContainer
{
    private static readonly Dictionary<Type, object> Services = new Dictionary<Type, object>();
    private static readonly object Lock = new object();
    private static bool frozen;

    /// <summary>
    /// Freeze the container — prevents further registrations.
    /// Call after bootstrap initialization is complete.
    /// </summary>
    public static void Freeze()
    {
        frozen = true;
    }

    /// <summary>
    /// Register a singleton service instance.
    /// Throws if the type is already registered or container is frozen.
    /// </summary>
    public static void Register<T>(T instance) where T : class
    {
        if (frozen)
        {
            Debug.LogError($"[ServiceContainer] Cannot register {typeof(T).Name} — container is frozen");
            return;
        }

        lock (Lock)
        {
            if (Services.ContainsKey(typeof(T)))
            {
                Debug.LogError($"[ServiceContainer] Duplicate registration for {typeof(T).Name}");
                return;
            }
            Services[typeof(T)] = instance;
        }
    }

    /// <summary>
    /// Register a service with a factory function (lazy creation).
    /// Factory is invoked immediately during registration.
    /// </summary>
    public static void RegisterLazy<T>(Func<T> factory) where T : class
    {
        Register(factory());
    }

    /// <summary>
    /// Resolve a registered service. Returns null if not found.
    /// Thread-safe — reads from dictionary without lock after frozen.
    /// </summary>
    public static T Resolve<T>() where T : class
    {
        if (Services.TryGetValue(typeof(T), out object instance))
        {
            return (T)instance;
        }

        Debug.LogError($"[ServiceContainer] Service {typeof(T).Name} not registered");
        return null;
    }

    /// <summary>
    /// Try to resolve a service — returns false if not found without logging error.
    /// </summary>
    public static bool TryResolve<T>(out T service) where T : class
    {
        if (Services.TryGetValue(typeof(T), out object instance))
        {
            service = (T)instance;
            return true;
        }

        service = null;
        return false;
    }

    /// <summary>
    /// Clear all registrations (for cleanup or scene reload).
    /// </summary>
    public static void Clear()
    {
        lock (Lock)
        {
            Services.Clear();
            frozen = false;
        }
    }
}