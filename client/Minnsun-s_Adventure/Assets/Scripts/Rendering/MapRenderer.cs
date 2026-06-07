using UnityEngine;

/// <summary>
/// Procedurally generates a ground plane with a grid texture at runtime.
/// No assets, no editor setup — fully code-driven.
/// 
/// - Creates a 1000x1000 flat mesh plane at Y = -0.5f
/// - Generates a Texture2D grid pattern with lines every 10 units
/// - Uses Universal Render Pipeline/Lit shader for shadow receiving
/// </summary>
public class MapRenderer : MonoBehaviour
{
    [Header("Ground")]
    public float groundSize = 1000f;
    public float groundY = -0.5f;

    [Header("Grid Texture")]
    public int textureResolution = 1024;
    public int gridCells = 100;              // 100 cells across → each cell = 10 world units
    public Color gridLineColor = new Color(0.2f, 0.2f, 0.2f, 0.8f);
    public Color gridBgColor = new Color(0.12f, 0.12f, 0.12f, 1f);
    public int lineWidthPx = 2;

    [Header("Material")]
    public string shaderName = "Universal Render Pipeline/Lit";

    // ─── Public API ──────────────────────────────────────────────────────

    /// <summary>
    /// Build the ground grid. Call once at bootstrap.
    /// </summary>
    public void Build()
    {
        // Generate grid texture procedurally
        Texture2D gridTex = GenerateGridTexture();

        // Create material with URP Lit shader
        Shader shader = Shader.Find(shaderName);
        if (shader == null)
        {
            Logger.W("MapRenderer", "Shader '{0}' not found, falling back to Standard", shaderName);
            shader = Shader.Find("Standard");
        }
        Material mat = new Material(shader);
        mat.mainTexture = gridTex;
        mat.mainTextureScale = new Vector2(1, 1);
        mat.mainTextureOffset = new Vector2(0, 0);
        mat.enableInstancing = true;

        // Enable shadow receiving (URP Lit does this by default, but be explicit)
        mat.SetFloat("_ReceiveShadows", 1.0f);

        // Create mesh: a simple flat quad (2 triangles)
        Mesh mesh = CreateGroundMesh();

        // Build GameObject
        GameObject groundGO = new GameObject("Ground");
        groundGO.transform.SetParent(transform);
        groundGO.transform.position = new Vector3(0, groundY, 0);
        groundGO.transform.rotation = Quaternion.identity;

        MeshFilter mf = groundGO.AddComponent<MeshFilter>();
        mf.mesh = mesh;

        MeshRenderer mr = groundGO.AddComponent<MeshRenderer>();
        mr.material = mat;
        mr.shadowCastingMode = UnityEngine.Rendering.ShadowCastingMode.Off;  // ground doesn't cast
        mr.receiveShadows = true;

        // Add mesh collider so click/raycast interactions work
        MeshCollider mc = groundGO.AddComponent<MeshCollider>();
        mc.sharedMesh = mesh;

        Logger.I("MapRenderer", "Ground grid built: {0}x{0} at Y={1}, grid cells={2}", groundSize, groundY, gridCells);
    }

    // ─── Mesh Generation ─────────────────────────────────────────────────

    /// <summary>
    /// Creates a flat quad mesh of groundSize x groundSize centered at origin.
    /// </summary>
    private Mesh CreateGroundMesh()
    {
        float half = groundSize * 0.5f;

        Vector3[] vertices = new Vector3[]
        {
            new Vector3(-half, 0, -half),
            new Vector3( half, 0, -half),
            new Vector3(-half, 0,  half),
            new Vector3( half, 0,  half)
        };

        int[] triangles = new int[]
        {
            0, 2, 1,
            2, 3, 1
        };

        Vector2[] uv = new Vector2[]
        {
            new Vector2(0, 0),
            new Vector2(1, 0),
            new Vector2(0, 1),
            new Vector2(1, 1)
        };

        Mesh mesh = new Mesh();
        mesh.name = "GroundMesh";
        mesh.vertices = vertices;
        mesh.triangles = triangles;
        mesh.uv = uv;
        mesh.RecalculateNormals();
        mesh.RecalculateBounds();

        return mesh;
    }

    // ─── Grid Texture Generation ─────────────────────────────────────────

    /// <summary>
    /// Generates a Texture2D with a grid pattern.
    /// Line color = gridLineColor, background = gridBgColor.
    /// </summary>
    private Texture2D GenerateGridTexture()
    {
        int res = Mathf.Clamp(textureResolution, 64, 4096);
        Texture2D tex = new Texture2D(res, res, TextureFormat.RGBA32, false, true);
        tex.name = "GridTexture";
        tex.wrapMode = TextureWrapMode.Clamp;
        tex.filterMode = FilterMode.Point;
        tex.anisoLevel = 0;

        // Pre-calculate line threshold
        float cellSize = (float)res / gridCells;
        float lineThreshold = lineWidthPx;

        Color[] pixels = new Color[res * res];

        for (int y = 0; y < res; y++)
        {
            for (int x = 0; x < res; x++)
            {
                // Determine if this pixel is on a grid line
                float cx = x % cellSize;
                float cy = y % cellSize;

                bool isLine = (cx < lineThreshold) || (cy < lineThreshold);

                // Also draw border lines at the edges
                if (x < lineWidthPx || y < lineWidthPx || x >= res - lineWidthPx || y >= res - lineWidthPx)
                    isLine = true;

                pixels[y * res + x] = isLine ? gridLineColor : gridBgColor;
            }
        }

        tex.SetPixels(pixels);
        tex.Apply(false, true); // upload to GPU, mark as non-readable (no more CPU access)

        Logger.D("MapRenderer", "Grid texture generated: {0}x{0}, cells={1}, lineWidth={2}px", res, gridCells, lineWidthPx);
        return tex;
    }
}