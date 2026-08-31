export declare const CYMONKEY_PROTOCOL_VERSION = "jangolova.cymonkey/v1alpha2";
export declare const CAMERA_KIT_RUNTIME = "snapchat-camera-kit";
export declare const CAMERA_KIT_DRIVER = "sandbox";
export declare const CYMONKEY_RUNTIME_SYMBOL: unique symbol;
export type CymonkeyRequest = {
    id?: unknown;
    method: string;
    params?: Record<string, unknown>;
};
export type CameraKitCymonkeyOptions = {
    augmentationId: string;
    apiToken: string;
    title?: string;
    mediaProvider?: CameraMediaProvider;
};
export type CameraMediaProvider = {
    openCamera(): Promise<MediaStream>;
    closeCamera(): Promise<void>;
};
export declare class CameraKitCymonkey {
    #private;
    readonly protocolVersion = "jangolova.cymonkey/v1alpha2";
    readonly augmentationId: string;
    readonly apiToken: string;
    readonly title: string;
    readonly mediaProvider: CameraMediaProvider;
    constructor(options: CameraKitCymonkeyOptions);
    installGlobal(target?: Record<PropertyKey, unknown>): () => void;
    hello(): {
        protocolVersion: string;
        implementation: {
            name: string;
            version: string;
        };
        domains: string[];
        runtimes: string[];
        drivers: string[];
        features: string[];
    };
    capabilities(): {
        name: string;
        domain: string;
        runtime: string;
        driver: string;
        support: string;
        lifetime: string;
        persistence: string;
        effect: string;
        inputSchema: {
            type: string;
            additionalProperties: boolean;
        };
    }[];
    describe(): {
        revision: string;
        surfaces: {
            id: string;
            domain: string;
            runtime: string;
            kind: string;
            label: string;
            properties: {
                started: boolean;
                cameraPermission: string;
            };
            actions: string[];
        }[];
        augmentations: {
            id: string;
            enabled: boolean;
        }[];
    };
    dispatch(request: CymonkeyRequest): Promise<{
        id: {} | null;
        result: unknown;
    } | {
        id: {} | null;
        error: {
            code: string;
            message: string;
        };
    }>;
    act(params: Record<string, unknown>): Promise<{
        revision: string;
        surfaces: {
            id: string;
            domain: string;
            runtime: string;
            kind: string;
            label: string;
            properties: {
                started: boolean;
                cameraPermission: string;
            };
            actions: string[];
        }[];
        augmentations: {
            id: string;
            enabled: boolean;
        }[];
    } | {
        ok: boolean;
    }>;
    mount(input: Record<string, unknown>): {
        ok: boolean;
        userActionRequired: boolean;
        surfaceId: string;
    };
    applyLens(input: Record<string, unknown>): Promise<{
        ok: boolean;
        lensId: string;
        lensGroupId: string;
    }>;
    removeLens(): Promise<{
        ok: boolean;
    }>;
    stopCamera(): Promise<{
        ok: boolean;
        stopped: boolean;
    }>;
    unmount(): Promise<{
        ok: boolean;
        removed: boolean;
    }>;
    private startFromUserGesture;
    private loadCameraKit;
    private requireCameraKit;
    private requireStarted;
}
