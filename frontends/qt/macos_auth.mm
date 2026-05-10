// SPDX-License-Identifier: Apache-2.0

#include "macos_auth.h"
#include "libserver.h"

#if defined(__APPLE__)
#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <dispatch/dispatch.h>
#endif

#if defined(__APPLE__)
static int classifyAuthError(NSError* error) {
    if (error == nil) {
        return 3;
    }
    switch (error.code) {
    case LAErrorUserCancel:
    case LAErrorSystemCancel:
    case LAErrorAppCancel:
    case LAErrorUserFallback:
        return 1;
    case LAErrorBiometryNotAvailable:
    case LAErrorBiometryNotEnrolled:
    case LAErrorPasscodeNotSet:
        return 2;
    default:
        return 3;
    }
}

static void logAuthError(const char* stage, NSError* error) {
    if (error == nil) {
        goLog("macos-auth: no NSError");
        return;
    }
    NSString* msg = [NSString stringWithFormat:@"macos-auth:%s domain=%@ code=%ld desc=%@",
                     stage,
                     error.domain ?: @"<nil>",
                     (long)error.code,
                     error.localizedDescription ?: @"<nil>"];
    goLog(msg.UTF8String);
}
#endif

int macosAuthenticate(const char* reason) {
#if defined(__APPLE__)
    @autoreleasepool {
        NSString* localizedReason = @"Authenticate to unlock BitBoxApp";
        if (reason != nullptr) {
            localizedReason = [NSString stringWithUTF8String:reason];
        }

        __block int result = 3;
        dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

        void (^evaluate)(void) = ^{
            LAContext* context = [[LAContext alloc] init];
            NSError* canEvaluateError = nil;
            if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&canEvaluateError]) {
                logAuthError("canEvaluatePolicy", canEvaluateError);
                result = classifyAuthError(canEvaluateError);
                dispatch_semaphore_signal(semaphore);
                return;
            }

            [context evaluatePolicy:LAPolicyDeviceOwnerAuthentication
                    localizedReason:localizedReason
                              reply:^(BOOL success, NSError* evaluateError) {
                if (success) {
                    result = 0;
                } else {
                    logAuthError("evaluatePolicy", evaluateError);
                    result = classifyAuthError(evaluateError);
                }
                dispatch_semaphore_signal(semaphore);
            }];
        };

        if ([NSThread isMainThread]) {
            evaluate();
        } else {
            dispatch_async(dispatch_get_main_queue(), evaluate);
        }

        dispatch_semaphore_wait(semaphore, DISPATCH_TIME_FOREVER);
        return result;
    }
#else
    (void)reason;
    return 2;
#endif
}
