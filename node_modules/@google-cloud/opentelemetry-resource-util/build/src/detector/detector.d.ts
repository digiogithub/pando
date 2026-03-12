import { DetectedResource, ResourceDetector } from '@opentelemetry/resources';
/**
 * Google Cloud resource detector which populates attributes based on the environment this
 * process is running in. If not on GCP, returns an empty resource.
 */
export declare class GcpDetectorSync implements ResourceDetector {
    private _asyncAttributes;
    detect(): DetectedResource;
}
