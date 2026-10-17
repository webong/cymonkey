using UnityEngine;

namespace Jangolova.Unity
{
    [DisallowMultipleComponent]
    public sealed class JangolovaObject : MonoBehaviour
    {
        [SerializeField] private string objectId;
        [SerializeField] private string objectType;

        public string ObjectId
        {
            get { return objectId; }
        }

        public string ObjectType
        {
            get { return objectType; }
        }

        internal void Initialize(string id, string type)
        {
            objectId = id;
            objectType = type;
        }
    }
}
