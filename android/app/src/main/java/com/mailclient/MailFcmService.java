package com.mailclient;

import android.app.Notification;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;

import androidx.annotation.NonNull;
import androidx.core.app.NotificationCompat;

import com.google.firebase.messaging.FirebaseMessagingService;
import com.google.firebase.messaging.RemoteMessage;

import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Handles FCM push notifications for new emails.
 * Mirrors AppDelegate push handling in the iOS client.
 */
public class MailFcmService extends FirebaseMessagingService {

    private static final AtomicInteger notifIdCounter = new AtomicInteger(1);

    @Override
    public void onNewToken(@NonNull String token) {
        super.onNewToken(token);
        // Forward to MailViewModel via a broadcast so it can register the new token.
        Intent intent = new Intent("com.mailclient.FCM_TOKEN_UPDATED");
        intent.putExtra("token", token);
        sendBroadcast(intent);
    }

    @Override
    public void onMessageReceived(@NonNull RemoteMessage message) {
        super.onMessageReceived(message);

        Map<String, String> data = message.getData();
        String accountId = data.get("account_id");
        String emailId   = data.get("email_id");
        String subject   = data.get("subject");
        String from      = data.get("from");

        String title = subject != null && !subject.isEmpty() ? subject : getString(R.string.new_email);
        String body  = from != null ? from : "";

        // Tapping the notification deep-links to the specific email.
        Intent tapIntent = new Intent(this, MainActivity.class);
        tapIntent.putExtra("account_id", accountId);
        tapIntent.putExtra("email_id", emailId);
        tapIntent.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent pi = PendingIntent.getActivity(this, notifIdCounter.get(),
                tapIntent, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        Notification n = new NotificationCompat.Builder(this, "new_email")
                .setSmallIcon(android.R.drawable.ic_dialog_email)
                .setContentTitle(title)
                .setContentText(body)
                .setAutoCancel(true)
                .setContentIntent(pi)
                .setGroup("mail_group")
                .build();

        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        if (nm != null) nm.notify(notifIdCounter.getAndIncrement(), n);
    }
}
