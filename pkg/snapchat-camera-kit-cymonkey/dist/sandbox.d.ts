type Context = {
    augmentationId: string;
    configuration: Record<string, unknown>;
};
type SandboxPackage = {
    id: string;
    connect(context: Context, port: MessagePort): void;
};
declare global {
    interface Window {
        jangolovaSandboxPackage?: SandboxPackage;
    }
}
export {};
