using System;
using UnityEditor;
using UnityEditor.Build.Reporting;
using UnityEngine;

public static class BuildScript
{
    [MenuItem("Build/WebGL (CLI)")]
    public static void Build()
    {
        // Extract -buildOutput from command line arguments.
        string outputPath = null;
        string[] args = Environment.GetCommandLineArgs();
        for (int i = 0; i < args.Length - 1; i++)
        {
            if (args[i].Equals("-buildOutput", StringComparison.OrdinalIgnoreCase))
            {
                outputPath = args[i + 1];
                break;
            }
        }

        if (string.IsNullOrEmpty(outputPath))
        {
            Debug.LogError("[BuildScript] Missing -buildOutput argument");
            EditorApplication.Exit(1);
            return;
        }

        Console.WriteLine("[BuildScript] Starting WebGL build to: " + outputPath);

        // For Standalone builds, locationPathName must include the .exe filename.
        string exePath = System.IO.Path.Combine(outputPath, "Minnsun-s_Adventure.exe");
        Debug.Log("[BuildScript] Exe path: " + exePath);

        BuildPlayerOptions buildPlayerOptions = new BuildPlayerOptions
        {
            scenes = GetEnabledScenes(),
            locationPathName = exePath,
            target = BuildTarget.StandaloneWindows64,
            options = BuildOptions.None,
        };

        BuildReport report = BuildPipeline.BuildPlayer(buildPlayerOptions);
        BuildSummary summary = report.summary;

        if (summary.result == BuildResult.Succeeded)
        {
            Console.WriteLine("[BuildScript] WebGL build succeeded: " + outputPath);
            EditorApplication.Exit(0);
        }
        else
        {
            string errors = "";
            foreach (var step in report.steps)
            {
                foreach (var msg in step.messages)
                {
                    if (msg.type == LogType.Error || msg.type == LogType.Exception)
                    {
                        errors += msg.content + "\n";
                    }
                }
            }
            Console.Error.WriteLine("[BuildScript] WebGL build FAILED (exit code 1)");
            Console.Error.WriteLine("[BuildScript] Errors:\n" + errors);
            EditorApplication.Exit(1);
        }
    }

    private static string[] GetEnabledScenes()
    {
        var scenes = new System.Collections.Generic.List<string>();
        foreach (var scene in EditorBuildSettings.scenes)
        {
            if (scene.enabled)
            {
                scenes.Add(scene.path);
            }
        }
        // Fallback: if no scenes are enabled, add all.
        if (scenes.Count == 0)
        {
            foreach (var scene in EditorBuildSettings.scenes)
            {
                scenes.Add(scene.path);
            }
        }
        return scenes.ToArray();
    }
}