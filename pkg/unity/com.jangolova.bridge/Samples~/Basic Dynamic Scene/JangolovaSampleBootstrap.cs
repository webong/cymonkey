using Jangolova.Unity;
using UnityEngine;

public sealed class JangolovaSampleBootstrap : MonoBehaviour
{
    private void Awake()
    {
        if (FindObjectOfType<JangolovaSceneBridge>() == null)
            gameObject.AddComponent<JangolovaSceneBridge>();

        if (Camera.main == null)
        {
            GameObject cameraObject = new GameObject("Main Camera");
            cameraObject.tag = "MainCamera";
            Camera camera = cameraObject.AddComponent<Camera>();
            camera.transform.position = new Vector3(0, 3, -8);
            camera.transform.LookAt(Vector3.zero);
        }

        if (FindObjectOfType<Light>() == null)
        {
            GameObject lightObject = new GameObject("Directional Light");
            Light light = lightObject.AddComponent<Light>();
            light.type = LightType.Directional;
            light.transform.rotation = Quaternion.Euler(45, -30, 0);
        }
    }
}
