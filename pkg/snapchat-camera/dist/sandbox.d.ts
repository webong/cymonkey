type Context = {
    augmentationId: string;
    configuration: Record<string, unknown>;
};
type SandboxPackage = {
    id: string;
    connect(context: Context, port: MessagePort): Promise<void>;
};
declare global {
    interface Window {
        cymonkeySandboxPackage?: SandboxPackage;
    }
}
export {};
