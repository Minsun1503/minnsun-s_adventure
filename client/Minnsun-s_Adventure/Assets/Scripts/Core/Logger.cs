using System;
using UnityEngine;

/// <summary>
/// Log level enum for runtime filtering.
/// </summary>
public enum LogLevel
{
    Debug = 0,
    Info  = 1,
    Warn  = 2,
    Error = 3,
    None  = 4
}

/// <summary>
/// Centralized logger with level filtering and category prefixes.
/// In editor defaults to Debug; in release builds defaults to Info.
/// Can be overridden at runtime via <see cref="MinLevel"/> or config.
///
/// Usage:
///   Logger.D("Network", "Connected to {0}", host);
///   Logger.I("Bootstrap", "System initialized");
///   Logger.W("Entity", "Duplicate spawn detected");
///   Logger.E("Network", "Connection lost: {0}", ex.Message);
/// </summary>
public static class Logger
{
    /// <summary>Minimum log level to actually print. Set from config at bootstrap.</summary>
    public static LogLevel MinLevel { get; set; } = DebugLevelDefault();

    /// <summary>Optional tag filtering — if set, only logs with these category prefixes pass through.</summary>
    public static string[] AllowedCategories { get; set; } = null;

    private static LogLevel DebugLevelDefault()
    {
#if DEVELOPMENT_BUILD || UNITY_EDITOR
        return LogLevel.Debug;
#else
        return LogLevel.Info;
#endif
    }

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>Debug-level log (only printed when MinLevel <= Debug).</summary>
    public static void D(string category, string message)
    {
        if (MinLevel <= LogLevel.Debug && PassesFilter(category))
            Debug.Log(Format("DEBUG", category, message));
    }

    /// <summary>Debug-level log with format args.</summary>
    public static void D(string category, string format, params object[] args)
    {
        if (MinLevel <= LogLevel.Debug && PassesFilter(category))
            Debug.Log(Format("DEBUG", category, string.Format(format, args)));
    }

    /// <summary>Info-level log.</summary>
    public static void I(string category, string message)
    {
        if (MinLevel <= LogLevel.Info && PassesFilter(category))
            Debug.Log(Format("INFO", category, message));
    }

    /// <summary>Info-level log with format args.</summary>
    public static void I(string category, string format, params object[] args)
    {
        if (MinLevel <= LogLevel.Info && PassesFilter(category))
            Debug.Log(Format("INFO", category, string.Format(format, args)));
    }

    /// <summary>Warning-level log.</summary>
    public static void W(string category, string message)
    {
        if (MinLevel <= LogLevel.Warn && PassesFilter(category))
            Debug.LogWarning(Format("WARN", category, message));
    }

    /// <summary>Warning-level log with format args.</summary>
    public static void W(string category, string format, params object[] args)
    {
        if (MinLevel <= LogLevel.Warn && PassesFilter(category))
            Debug.LogWarning(Format("WARN", category, string.Format(format, args)));
    }

    /// <summary>Error-level log.</summary>
    public static void E(string category, string message)
    {
        if (MinLevel <= LogLevel.Error && PassesFilter(category))
            Debug.LogError(Format("ERROR", category, message));
    }

    /// <summary>Error-level log with format args.</summary>
    public static void E(string category, string format, params object[] args)
    {
        if (MinLevel <= LogLevel.Error && PassesFilter(category))
            Debug.LogError(Format("ERROR", category, string.Format(format, args)));
    }

    // ─── Internals ──────────────────────────────────────────────────────

    private static string Format(string level, string category, string message)
    {
        return $"[{level}] [{category}] {message}";
    }

    /// <summary>
    /// If AllowedCategories is set, only categories starting with any of the allowed prefixes pass.
    /// Null or empty AllowedCategories means all categories pass.
    /// </summary>
    private static bool PassesFilter(string category)
    {
        if (AllowedCategories == null || AllowedCategories.Length == 0)
            return true;

        for (int i = 0; i < AllowedCategories.Length; i++)
        {
            if (category.StartsWith(AllowedCategories[i], StringComparison.Ordinal))
                return true;
        }
        return false;
    }
}