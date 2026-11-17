package app.unison.storage

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import android.util.Log
import app.unison.service.UnisonSettings

/**
 * The folder the user picked for received files. Access comes from the system
 * folder picker and covers that one folder only, so the app needs no storage
 * permission for it.
 */
object DownloadsFolder {
    private const val FLAGS =
        Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION

    /** The picked folder when the app still holds write access to it. */
    fun current(context: Context): Uri? {
        val stored = UnisonSettings.downloadsTree(context) ?: return null
        val tree = Uri.parse(stored)
        val granted = context.contentResolver.persistedUriPermissions
            .any { it.uri == tree && it.isWritePermission }
        if (!granted) {
            Log.w(TAG, "access to the chosen folder is gone, the default folder is used")
            return null
        }
        return tree
    }

    /** Keeps the access to a picked folder across restarts and forgets the previous folder. */
    fun choose(context: Context, tree: Uri): Boolean {
        try {
            context.contentResolver.takePersistableUriPermission(tree, FLAGS)
        } catch (e: SecurityException) {
            Log.w(TAG, "cannot keep access to the folder: ${e.message}")
            return false
        }
        release(context)
        UnisonSettings.setDownloadsTree(context, tree.toString())
        return true
    }

    /** Goes back to the default folder and gives the access back to the system. */
    fun reset(context: Context) {
        release(context)
        UnisonSettings.setDownloadsTree(context, null)
    }

    /** Readable path of a picked folder, like `Documents/Inbox`. */
    fun label(tree: Uri): String {
        val id = try {
            DocumentsContract.getTreeDocumentId(tree)
        } catch (e: IllegalArgumentException) {
            return tree.toString()
        }
        val path = id.substringAfter(':', "")
        return path.ifEmpty { id.substringBefore(':') }
    }

    /** Creates an empty file inside the folder and returns its URI. */
    fun create(context: Context, tree: Uri, name: String, mimeType: String): Uri? {
        val parent = DocumentsContract.buildDocumentUriUsingTree(
            tree,
            DocumentsContract.getTreeDocumentId(tree),
        )
        return DocumentsContract.createDocument(context.contentResolver, parent, mimeType, name)
    }

    private fun release(context: Context) {
        val previous = UnisonSettings.downloadsTree(context) ?: return
        try {
            context.contentResolver.releasePersistableUriPermission(Uri.parse(previous), FLAGS)
        } catch (e: SecurityException) {
            Log.w(TAG, "cannot release the previous folder: ${e.message}")
        }
    }

    private const val TAG = "DownloadsFolder"
}
