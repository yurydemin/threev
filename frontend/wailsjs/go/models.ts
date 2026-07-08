export namespace domain {
	
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

}

