package com.mailclient.network;

/** Generic async callback used throughout ApiClient. */
public interface ApiCallback<T> {
    void onSuccess(T result);
    void onFailure(String error);
}
