import { Resource } from '@opentelemetry/resources';
type Labels = {
    [key: string]: string;
};
export interface MonitoredResource {
    type: string;
    labels: Labels;
}
/**
 * Given an OTel resource, return a MonitoredResource. Copied from the collector's
 * implementation in Go:
 * https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/internal/resourcemapping/resourcemapping.go#L51
 *
 * @param resource the OTel Resource
 * @returns the corresponding GCM MonitoredResource
 */
export declare function mapOtelResourceToMonitoredResource(resource: Resource): MonitoredResource;
export { GcpDetectorSync } from './detector/detector';
