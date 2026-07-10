export namespace domain {
	
	export class Bucket {
	    Name: string;
	    // Go type: time
	    CreationDate: any;
	
	    static createFrom(source: any = {}) {
	        return new Bucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.CreationDate = this.convertValues(source["CreationDate"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConnectionTestResult {
	    Success: boolean;
	    Message: string;
	    Detail: string;
	    Category: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Success = source["Success"];
	        this.Message = source["Message"];
	        this.Detail = source["Detail"];
	        this.Category = source["Category"];
	    }
	}
	export class DownloadRequest {
	    ProfileID: number;
	    Bucket: string;
	    Key: string;
	    LocalPath: string;
	    Priority: number;
	
	    static createFrom(source: any = {}) {
	        return new DownloadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ProfileID = source["ProfileID"];
	        this.Bucket = source["Bucket"];
	        this.Key = source["Key"];
	        this.LocalPath = source["LocalPath"];
	        this.Priority = source["Priority"];
	    }
	}
	export class ListObjectsRequest {
	    ProfileID: number;
	    Bucket: string;
	    Prefix: string;
	    ContinuationToken: string;
	    SortBy: string;
	    SortOrder: string;
	    Refresh: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListObjectsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ProfileID = source["ProfileID"];
	        this.Bucket = source["Bucket"];
	        this.Prefix = source["Prefix"];
	        this.ContinuationToken = source["ContinuationToken"];
	        this.SortBy = source["SortBy"];
	        this.SortOrder = source["SortOrder"];
	        this.Refresh = source["Refresh"];
	    }
	}
	export class ObjectEntry {
	    Key: string;
	    IsFolder: boolean;
	    Size: number;
	    ContentType: string;
	    // Go type: time
	    LastModified: any;
	    StorageClass: string;
	
	    static createFrom(source: any = {}) {
	        return new ObjectEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.IsFolder = source["IsFolder"];
	        this.Size = source["Size"];
	        this.ContentType = source["ContentType"];
	        this.LastModified = this.convertValues(source["LastModified"], null);
	        this.StorageClass = source["StorageClass"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ListObjectsResponse {
	    Entries: ObjectEntry[];
	    NextContinuationToken: string;
	    IsTruncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListObjectsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Entries = this.convertValues(source["Entries"], ObjectEntry);
	        this.NextContinuationToken = source["NextContinuationToken"];
	        this.IsTruncated = source["IsTruncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ObjectMeta {
	    Key: string;
	    Size: number;
	    ContentType: string;
	    ETag: string;
	    // Go type: time
	    LastModified: any;
	    Metadata: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ObjectMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Size = source["Size"];
	        this.ContentType = source["ContentType"];
	        this.ETag = source["ETag"];
	        this.LastModified = this.convertValues(source["LastModified"], null);
	        this.Metadata = source["Metadata"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Profile {
	    ID: number;
	    Name: string;
	    EndpointURL: string;
	    Region: string;
	    AccessKeyID: string;
	    SecretAccessKey: string;
	    SessionToken: string;
	    PathStyle: boolean;
	    VerifySSL: boolean;
	    CustomHeaders: Record<string, string>;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.EndpointURL = source["EndpointURL"];
	        this.Region = source["Region"];
	        this.AccessKeyID = source["AccessKeyID"];
	        this.SecretAccessKey = source["SecretAccessKey"];
	        this.SessionToken = source["SessionToken"];
	        this.PathStyle = source["PathStyle"];
	        this.VerifySSL = source["VerifySSL"];
	        this.CustomHeaders = source["CustomHeaders"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProfileDTO {
	    ID: number;
	    Name: string;
	    EndpointURL: string;
	    Region: string;
	    PathStyle: boolean;
	    VerifySSL: boolean;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ProfileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.EndpointURL = source["EndpointURL"];
	        this.Region = source["Region"];
	        this.PathStyle = source["PathStyle"];
	        this.VerifySSL = source["VerifySSL"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TextPreviewResult {
	    Content: string;
	    Truncated: boolean;
	    TotalSize: number;
	
	    static createFrom(source: any = {}) {
	        return new TextPreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Content = source["Content"];
	        this.Truncated = source["Truncated"];
	        this.TotalSize = source["TotalSize"];
	    }
	}
	export class TransferHistoryEntry {
	    ID: number;
	    QueueID: number;
	    ProfileID: number;
	    Type: string;
	    SourcePath: string;
	    DestinationPath: string;
	    TotalBytes: number;
	    Status: string;
	    // Go type: time
	    CompletedAt: any;
	    ErrorMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new TransferHistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.QueueID = source["QueueID"];
	        this.ProfileID = source["ProfileID"];
	        this.Type = source["Type"];
	        this.SourcePath = source["SourcePath"];
	        this.DestinationPath = source["DestinationPath"];
	        this.TotalBytes = source["TotalBytes"];
	        this.Status = source["Status"];
	        this.CompletedAt = this.convertValues(source["CompletedAt"], null);
	        this.ErrorMessage = source["ErrorMessage"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TransferTask {
	    ID: number;
	    ProfileID: number;
	    Type: string;
	    SourcePath: string;
	    DestinationPath: string;
	    Status: string;
	    TotalBytes: number;
	    TransferredBytes: number;
	    ErrorMessage: string;
	    MultipartUploadID: string;
	    PartsCompleted: string;
	    FileOffset: number;
	    Priority: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new TransferTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.ProfileID = source["ProfileID"];
	        this.Type = source["Type"];
	        this.SourcePath = source["SourcePath"];
	        this.DestinationPath = source["DestinationPath"];
	        this.Status = source["Status"];
	        this.TotalBytes = source["TotalBytes"];
	        this.TransferredBytes = source["TransferredBytes"];
	        this.ErrorMessage = source["ErrorMessage"];
	        this.MultipartUploadID = source["MultipartUploadID"];
	        this.PartsCompleted = source["PartsCompleted"];
	        this.FileOffset = source["FileOffset"];
	        this.Priority = source["Priority"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UploadRequest {
	    ProfileID: number;
	    Bucket: string;
	    Key: string;
	    LocalPath: string;
	    Priority: number;
	
	    static createFrom(source: any = {}) {
	        return new UploadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ProfileID = source["ProfileID"];
	        this.Bucket = source["Bucket"];
	        this.Key = source["Key"];
	        this.LocalPath = source["LocalPath"];
	        this.Priority = source["Priority"];
	    }
	}

}

